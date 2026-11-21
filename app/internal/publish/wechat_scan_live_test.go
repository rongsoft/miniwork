//go:build live

package publish

import (
	"strings"
	"testing"
	"time"
)

// 公众号扫码这条路，能自动验的只有前半段：页面能不能打开、二维码在不在、
// 轮询时会不会把它刷掉。真正的「扫完登录」要人拿手机扫，测不了。
//
// 但这前半段恰好是昨晚最容易写错的地方：状态栏每两秒轮询一次，
// 若每次轮询都导航一次，二维码就被换掉了——用户永远扫不上，
// 而且界面上看不出任何异常（页面好好的，就是扫不成功）。
//
// 跑法：go test -tags live ./internal/publish/ -run TestWeChatScan -count=1 -v
func TestWeChatScanKeepsQRCode(t *testing.T) {
	e := liveEngine(t)

	if err := e.WeChatOpenConsole(); err != nil {
		t.Fatalf("打开后台: %v", err)
	}
	time.Sleep(6 * time.Second) // 等二维码渲染

	url1, err := e.LiveLocation("wechat")
	if err != nil {
		t.Fatalf("读地址: %v", err)
	}
	if !strings.Contains(url1, "mp.weixin.qq.com") {
		t.Fatalf("没停在公众号域名上，实际 %s", url1)
	}

	// 二维码必须是「真的加载出来的一张图」，不能只数标签。
	// 微信那张图是 <img class="login__type__container__scan__qrcode">，
	// 请求失败时标签照样在、naturalWidth 是 0——这时候界面上是张白卡。
	qr, err := e.LiveEval("wechat", `(() => {
		const im = document.querySelector('.login__type__container__scan__qrcode');
		if (!im) return 'none';
		return 'w=' + im.naturalWidth + ' complete=' + im.complete + ' src=' + (im.src||'').slice(0,60);
	})()`)
	if err != nil {
		t.Fatalf("查二维码: %v", err)
	}
	t.Logf("二维码：%s", qr)
	if !strings.HasPrefix(qr, "w=") || strings.HasPrefix(qr, "w=0") {
		t.Errorf("二维码没加载出来（%s）——界面上会是一张白卡", qr)
	}

	// 关键一条：连轮询五次（界面就是这么干的），地址和页面都不能变。
	// 变了说明每次轮询都在重新导航，二维码被刷新掉了。
	for i := 0; i < 5; i++ {
		_, _ = e.WeChatAppID() // 界面轮询时调的就是它
		time.Sleep(800 * time.Millisecond)
		cur, err := e.LiveLocation("wechat")
		if err != nil {
			t.Fatalf("第 %d 次轮询后读地址失败: %v", i+1, err)
		}
		if cur != url1 {
			t.Fatalf("第 %d 次轮询把页面刷了：%s → %s（二维码会被换掉）", i+1, url1, cur)
		}
	}

	// 没登录时必须答 false，不能因为「浏览器开着」就说登录了。
	if e.Authed("wechat") {
		t.Errorf("还没扫码就答「已登录」")
	}
}
