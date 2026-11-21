package publish

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
)

// 公众号的「扫码绑定」只解决一件事：把人从「去后台翻 AppID」里救出来。
//
// 为什么不能像小红书那样扫码即用：公众号发布走的是接口，接口认的是
// AppID + AppSecret，不认浏览器登录态。扫码拿到的是后台会话（cookie），
// 它换不来 access_token。
//
// 所以这里的边界必须说清楚，否则就是骗人：
//   - AppID：扫码登录后从后台页面里读，能自动填；
//   - AppSecret：微信从不回显，页面上永远只有「已重置」三个字。
//     唯一能让它出现明文的办法是点「重置」，而重置会让旧密钥立刻失效
//     （用户如果还有别的系统在发同一个号，那边会当场断掉）。
//     这件事必须由人决定，所以界面上是「帮你打开那一页」，不是偷偷点。
//
// 顺带还省掉一件更麻烦的事：IP 白名单要填的是**调用方出口 IP**，
// 客户机上它是动态的。扫码之后我们至少能把这台机器当前看到的 IP
// 拿出来给他复制，不用他自己去搜索「我的 IP」。

const (
	// wxConsoleHome 是登录后的首页。地址里带 token，后面拼页面 URL 要用它。
	wxConsoleHome = "https://mp.weixin.qq.com/cgi-bin/home?t=home/index&lang=zh_CN"
	// wxDevPage 是「设置与开发 → 基本配置」。AppID 和 AppSecret 都在这一页，
	// 也是用户唯一需要自己动手的地方。
	wxDevPath = "/advanced/advanced?action=dev&t=advanced/dev&lang=zh_CN&token=%s"
)

var (
	wxTokenRe = regexp.MustCompile(`[?&]token=(\d+)`)
	// AppID 的形态是 wx + 16 位小写十六进制。原始 ID 是 gh_ 开头，
	// 微信号是自定义的，三者不会撞。
	wxAppIDRe = regexp.MustCompile(`\bwx[0-9a-f]{16}\b`)
)

// WeChatOpenConsole 打开公众号后台。
//
// 这里必须进根地址，不能进 /cgi-bin/home。
//
// 实测：没登录时访问 /cgi-bin/home?t=home/index，微信不回登录页，
// 而是甩一个「登录超时，请重新登录」的空白错误页 —— 页面上没有二维码，
// 用户点了「打开后台扫码」看到的是一句报错。根地址才会给二维码。
// 已登录时根地址自己会跳到带 token 的后台首页，不用我们判断。
func (e *Engine) WeChatOpenConsole() error {
	ctx, err := e.session("wechat")
	if err != nil {
		return err
	}
	p, ok := Get("wechat")
	if !ok || p.LoginURL == "" {
		return fmt.Errorf("公众号平台配置不全")
	}
	e.logf("正在打开公众号后台，用微信扫一下页面上的二维码")
	return chromedp.Run(ctx, chromedp.Navigate(p.LoginURL))
}

// WeChatAppID 尽力从后台页面里读出 AppID。
//
// 返回空串 + nil 表示「还没登录」或「页面里没有」——这两种都不该报错：
// 用户点开面板的那一刻本来就还没登录，那不是故障。
//
// 只有连浏览器都起不来才返回 error。
func (e *Engine) WeChatAppID() (string, error) {
	ctx, err := e.session("wechat")
	if err != nil {
		return "", err
	}

	token := e.weChatToken(ctx)
	if token == "" {
		return "", nil // 还没登录，地址里没有 token
	}

	// 先看当前页：用户可能自己已经翻到基本配置了，那就直接读。
	if id := e.grabAppID(ctx); id != "" {
		return id, nil
	}

	// 已经为这个 token 翻过一次就不再翻。
	//
	// 界面是每两秒轮询一次的，若每次都导航到基本配置页，右侧面板会
	// 一秒闪一下；用户正要点「重置」的时候页面跳走，比不帮他还难受。
	// 翻过之后只读——这一页的值是服务端渲染的，读不到就是读不到。
	e.wmu.Lock()
	tried := e.wxToken == token
	e.wmu.Unlock()
	if tried {
		return e.grabAppID(ctx), nil
	}

	// 再去基本配置页读一次。这是唯一一个 AppID 一定明文出现的地方。
	if err := chromedp.Run(ctx,
		chromedp.Navigate("https://mp.weixin.qq.com"+fmt.Sprintf(wxDevPath, token))); err != nil {
		return "", nil // 导航失败按"没读到"处理，不打断绑定流程
	}
	e.wmu.Lock()
	e.wxToken = token
	e.wmu.Unlock()
	// 这一页是服务端渲染后才把值写进 DOM 的，靠 waitAny 找不到明确的锚点
	// （AppID 没有固定 id），所以给一个短固定等待。1.2 秒是实测够用又不误事的值。
	time.Sleep(1200 * time.Millisecond)
	return e.grabAppID(ctx), nil
}

// weChatToken 从当前地址里取后台 token。
//
// 取不到就导航一次首页再取：用户刚扫完码时页面可能还停在登录页，
// 地址里没有 token，但会话已经有了——导航一次就补齐。
func (e *Engine) weChatToken(ctx context.Context) string {
	var cur string
	if err := chromedp.Run(ctx, chromedp.Location(&cur)); err == nil {
		if m := wxTokenRe.FindStringSubmatch(cur); len(m) == 2 {
			return m[1]
		}
		// 人已经在公众号域名上、地址里却没有 token，说明停在登录页。
		//
		// 这时候绝不能再导航一次：界面上那个二维码是活的，重载一次它就换一张，
		// 用户举着手机扫的可能是刚被换掉的那张，扫完提示「二维码失效」。
		// 轮询两秒一次，重载也两秒一次——他永远扫不上。
		if strings.Contains(cur, "mp.weixin.qq.com") {
			return ""
		}
	}
	// 真的是别的页面（用户自己点走了），拉回后台首页。
	if err := chromedp.Run(ctx, chromedp.Navigate(wxConsoleHome)); err != nil {
		return ""
	}
	time.Sleep(900 * time.Millisecond)
	cur = ""
	if err := chromedp.Run(ctx, chromedp.Location(&cur)); err != nil {
		return ""
	}
	if m := wxTokenRe.FindStringSubmatch(cur); len(m) == 2 {
		return m[1]
	}
	return ""
}

// grabAppID 在当前页面的 HTML 里找 AppID。
//
// 读 innerHTML 而不是 innerText：AppID 常常被放在 input 的 value 里，
// innerText 看不见它。
func (e *Engine) grabAppID(ctx context.Context) string {
	var html string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.documentElement.innerHTML`, &html)); err != nil {
		return ""
	}
	m := wxAppIDRe.FindString(strings.ToLower(html))
	return m
}
