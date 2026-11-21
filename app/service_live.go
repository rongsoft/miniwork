package main

import (
	"errors"
	"log"
	"net/url"
	"strings"
	"sync"
	"time"

	"rongsoft/miniwork/app/internal/publish"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// 平台网页在应用窗口里的那一屏。
//
// 对外只有六个动作：打开、关掉、改尺寸、投一次操作、跳地址、刷新。
// 界面那层不需要知道底下是 Chromium 还是别的——它只管把画面贴出来、
// 把用户的操作接着。

// liveMinFPS 是推给界面的帧率上限（约 18 帧/秒）。
//
// 浏览器那边满速能推 30 帧，每帧 50KB 上下。全量灌进界面通道的话，
// 前端每秒要做三十次「解码 70KB 的 base64 + 重绘」，滚动时肉眼可见地掉帧。
// 18 帧既能让滚动看起来连续，又把带宽砍掉四成。
const liveFrameGap = 55 * time.Millisecond

// liveGate 管帧节流。
//
// 单独一个结构而不是塞进 publishState：publishState 的锁护着发布任务，
// 而这里每秒要判十几次，混在一起会让「查发布进度」排在推帧后面。
type liveGate struct {
	mu   sync.Mutex
	last time.Time
	n    int
}

// allow 判断这一帧该不该发给界面。
func (g *liveGate) allow() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := time.Now()
	if !g.last.IsZero() && now.Sub(g.last) < liveFrameGap {
		return false
	}
	g.last = now
	g.n++
	return true
}

// LiveOpenInput 是打开那一屏的参数。
type LiveOpenInput struct {
	Platform string `json:"platform"`
	// URL 为空表示「接上画面，页面停在原处」。
	URL string `json:"url"`
	// W / H 是界面上那块区域的 CSS 尺寸。网页按这个尺寸排版，
	// 于是界面上的坐标就是页面里的坐标，点击不会偏。
	W int `json:"w"`
	H int `json:"h"`
}

// LiveView 是那一屏的当前状态。
type LiveView struct {
	Open     bool   `json:"open"`
	Platform string `json:"platform"`
	URL      string `json:"url"`
	Authed   bool   `json:"authed"`
	W        int    `json:"w"`
	H        int    `json:"h"`
	// Detached 表示这一屏已经拆到独立窗口里了。
	// 主窗口靠它决定「右栏是显示画面，还是显示一句『在另一个窗口里』」。
	Detached bool `json:"detached"`
}

// liveWinState 记着拆出去的那个窗口。
//
// 为什么要单独一个结构：窗口的关闭回调是异步的，用户按 X 的那一刻
// 和 Go 这边清状态的那一刻不在同一个线程上。用一把自己的锁管住
// 「窗口指针 + 这次关到底是收回还是关掉」，比在回调里读别处的状态可靠。
type liveWinState struct {
	mu  sync.Mutex
	win *application.WebviewWindow
	// quit 记着这次关闭是「关掉平台页」而不是「收回主窗口」。
	// 两者都要关窗口，但主窗口的反应正好相反：一个把画面接回去，
	// 一个把整栏收掉。
	quit bool
}

// LiveOpen 把平台页面接进应用窗口。
//
// 登录、发布都从这里进：以前是弹一个 Chromium 窗口出去，用户的第一反应
// 是「这软件在动我的浏览器」。现在网页渲染在我们自己的界面里，
// 但内核没换——扫码、传图、自动填表这些能力一步没少。
func (s *TopicService) LiveOpen(in LiveOpenInput) (LiveView, error) {
	out := LiveView{}
	plat := strings.TrimSpace(in.Platform)
	if plat == "" {
		return out, errors.New("没说打开哪个平台")
	}
	if s.app == nil {
		return out, errors.New("窗口还没准备好")
	}
	ps, err := s.publisher()
	if err != nil {
		return out, err
	}

	// 先定尺寸再开画面：反过来会先推几帧按旧尺寸排版的画面，
	// 用户看到的是页面「跳」一下才对齐。
	// 缩放比要在定尺寸之前给到引擎，否则第一批字就是虚的。
	ps.eng.SetDeviceScale(s.deviceScale())
	if in.W > 0 && in.H > 0 {
		_ = ps.eng.LiveResize(plat, in.W, in.H)
	}
	if err := ps.eng.LiveOpen(plat, in.URL); err != nil {
		return out, err
	}
	return s.liveView(ps, plat)
}

// LiveClose 关掉那一屏（嵌在窗口里的、拆出去的，一起关）。
func (s *TopicService) LiveClose() {
	if s.pub != nil && s.pub.eng != nil {
		s.pub.eng.LiveClose()
	}
	// 拆出去的窗口跟着关。这里先标记 quit 再关窗：关了之后主窗口
	// 该把整栏收掉，而不是把一块空画面接回来。
	s.liveWin.mu.Lock()
	w := s.liveWin.win
	s.liveWin.win = nil
	s.liveWin.quit = true
	s.liveWin.mu.Unlock()
	if w != nil {
		w.Close()
	}
}

// LiveDetach 把平台页面拆成一个独立窗口，像微信打开一篇文章那样。
//
// 为什么要拆：主窗口并排三栏时，中间那块只剩三百多像素。写稿时看不见
// 全的正文、发布时看不见全的表单，两边都憋屈。拆开之后各占一个窗口，
// 用户可以把它挪到另一块屏，主窗口整块留给自己的内容。
//
// 拆出去和收回共用同一个浏览器会话：登录态、正在填的表单、扫码进度
// 都在原来的 page 上，换的只是「画面推到哪个窗口」。
func (s *TopicService) LiveDetach(platform string) error {
	if s.app == nil {
		return errors.New("窗口还没准备好")
	}
	if s.pub == nil || s.pub.eng == nil {
		return errors.New("还没有打开平台页面")
	}
	plat := strings.TrimSpace(platform)
	if plat == "" {
		return errors.New("没说拆哪个平台")
	}

	s.liveWin.mu.Lock()
	if s.liveWin.win != nil {
		old := s.liveWin.win
		s.liveWin.mu.Unlock()
		old.Focus()
		return nil
	}

	name := plat
	if p, ok := publish.Get(plat); ok {
		name = p.Name
	}
	w := s.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:  "live",
		Title: "Miniwork — " + name,
		URL:   "/live.html?platform=" + url.QueryEscape(plat),
		// 默认按「能装下小红书发布表单」给：太窄会把表单挤成一列。
		Width:  1024,
		Height: 880,
		// 无边框：标题栏由 live.html 自己画。系统那条白色标题栏会把
		// 「这是应用里的一页」变成「这是另一个程序」，怎么调都不像成品。
		// 阴影和圆角仍然由系统给（DisableFramelessWindowDecorations 保持 false）。
		Frameless:        true,
		MinWidth:         460,
		MinHeight:        380,
		BackgroundColour: application.NewRGB(255, 253, 250),
	})
	s.liveWin.win = w
	s.liveWin.mu.Unlock()

	// 拆窗口这一步没法在界面里打印失败：窗口没出来时用户只看到
	// 「点了没反应」。留一条日志，排查时能分清是没开还是开在屏幕外。
	log.Printf("拆出平台窗口：%s（%s）", name, plat)

	// 关窗只代表「画面收回主窗口」：浏览器会话、登录态都还在，
	// 用户按 X 不等于把这个平台退出了。
	//
	// 注册放在解锁之后：hook 里要拿同一把锁，创建窗口的过程中
	// 如果同步触发了关闭事件，这里就会自己等自己。
	w.RegisterHook(events.Common.WindowClosing, func(*application.WindowEvent) {
		s.liveWin.mu.Lock()
		quit := s.liveWin.quit
		s.liveWin.quit = false
		s.liveWin.win = nil
		s.liveWin.mu.Unlock()
		s.emitDetached(quit)
	})
	return nil
}

// LiveAttach 把拆出去的窗口收回来。
//
// 关窗本身会发事件，主窗口收到就把右栏重新展开；这里只负责关。
func (s *TopicService) LiveAttach() {
	s.liveWin.mu.Lock()
	w := s.liveWin.win
	s.liveWin.win = nil
	s.liveWin.quit = false
	s.liveWin.mu.Unlock()
	if w != nil {
		w.Close()
	}
}

// LiveDetached 报当前是不是拆出去的状态。
//
// 拆出去之后主窗口那一栏会先收起（腾出宽度），刷新界面时得知道
// 「不是没打开，是开在另一个窗口里」，否则它会把画面重新接一遍。
func (s *TopicService) LiveDetached() bool {
	s.liveWin.mu.Lock()
	defer s.liveWin.mu.Unlock()
	return s.liveWin.win != nil
}

// emitDetached 告诉主窗口「拆出去的窗口开了 / 关了」。
//
// quit 为 true 表示这次关窗口等于「关掉平台页」，主窗口要收掉整栏；
// false 表示只是收回，画面该重新接回右栏。
func (s *TopicService) emitDetached(quit bool) {
	if s.app == nil {
		return
	}
	s.app.Event.Emit("live:detached", map[string]any{"open": false, "closed": quit})
}

// LiveResize 跟着界面上的区域改视口。
//
// 拖动窗口大小时会连着调很多次：这里不排队、不等待，直接设下去。
// 中间几次设的是过时的尺寸也无所谓，最后一次一定是对的。
func (s *TopicService) LiveResize(platform string, w, h int) error {
	if s.pub == nil || s.pub.eng == nil {
		return nil
	}
	s.pub.eng.SetDeviceScale(s.deviceScale())
	return s.pub.eng.LiveResize(platform, w, h)
}

// deviceScale 读界面所在屏幕的缩放比（Windows 的 125% / 150%）。
//
// 拿不到就按 1 算：宁可字小一点，也不能因为读 DPI 失败就不出画面。
func (s *TopicService) deviceScale() float64 {
	if s.app == nil {
		return 1
	}
	win := s.app.Window.Current()
	if win == nil {
		return 1
	}
	scr, err := win.GetScreen()
	if err != nil || scr == nil || scr.ScaleFactor <= 0 {
		return 1
	}
	return float64(scr.ScaleFactor)
}

// LiveInput 把用户在画面上的操作投回页面。
func (s *TopicService) LiveInput(platform string, ev publish.LiveInput) error {
	if s.pub == nil || s.pub.eng == nil {
		return errors.New("还没有打开平台页面")
	}
	return s.pub.eng.LiveInput(platform, ev)
}

// LiveNavigate 让页面跳地址。
func (s *TopicService) LiveNavigate(platform, url string) error {
	if s.pub == nil || s.pub.eng == nil {
		return errors.New("还没有打开平台页面")
	}
	if strings.TrimSpace(url) == "" {
		return errors.New("地址是空的")
	}
	return s.pub.eng.LiveNavigate(platform, url)
}

// LiveReload 刷新当前页。
func (s *TopicService) LiveReload(platform string) error {
	if s.pub == nil || s.pub.eng == nil {
		return errors.New("还没有打开平台页面")
	}
	return s.pub.eng.LiveReload(platform)
}

// LiveBack 回上一页。
func (s *TopicService) LiveBack(platform string) error {
	if s.pub == nil || s.pub.eng == nil {
		return errors.New("还没有打开平台页面")
	}
	return s.pub.eng.LiveBack(platform)
}

// WidenForLive 把主窗口撑到屏幕上放得下的宽度。
//
// 为什么要动窗口：并排三栏要的地方是死数——导航 260 + 自己的内容 400 +
// 平台页面 480。默认窗口 1040 宽，硬塞的话平台那一栏只剩三百出头，
// 小红书和抖音的桌面版页面在这么窄的条里没法用（表单挤成一列、按钮跑出屏幕）。
// 屏幕放不下就不动：宁可右栏窄一点，也不能把窗口推出屏外。
func (s *TopicService) WidenForLive() {
	if s.app == nil || s.sh == nil || s.sh.win == nil {
		return
	}
	scr := s.app.Screen.GetPrimary()
	if scr == nil {
		return
	}
	// 屏幕够宽时才撑。留 24 像素边距，贴着边线不好拖。
	//
	// 但封顶在 1440：再宽下去，右栏那块会跟着拉成一条长廊，
	// 网页两侧大片留白，反而比窄一点更难看。超宽屏上真正该用的是
	// 「拆出去」，那才是把平台页放到整块大屏上的正确做法。
	target := scr.WorkArea.Width - 24
	if target > 1440 {
		target = 1440
	}
	curW, curH := s.sh.win.Size()
	if curW <= 0 || target <= curW {
		return
	}
	// 只加宽、不加高：用户自己调过的高度是有理由的（比如放在副屏下半截）。
	if maxH := scr.WorkArea.Height - 24; curH > maxH {
		curH = maxH
	}
	x, y := s.sh.win.Position()
	if x+target > scr.WorkArea.X+scr.WorkArea.Width {
		x = scr.WorkArea.X + 12
	}
	s.sh.win.SetSize(target, curH)
	s.sh.win.SetPosition(x, y)
}

// LiveState 查那一屏现在停在哪、登录态如何。
//
// 界面靠它显示地址栏那一行：用户要能看见「现在停在小红书的登录页」
// 这种事，否则扫码扫一半跑偏了都不知道。
func (s *TopicService) LiveState(platform string) LiveView {
	if s.pub == nil || s.pub.eng == nil {
		return LiveView{}
	}
	v, _ := s.liveView(s.pub, platform)
	return v
}

// LiveLoginURL 给出某个平台的登录页地址。
//
// 地址存在 Go 这边（platform.go 那张表），界面不要自己拼：
// 平台换域名时只改一处，也不会有「界面拼错一个字符导致 404」这种事。
func (s *TopicService) LiveLoginURL(platform string) string {
	if p, ok := publish.Get(platform); ok {
		return p.LoginURL
	}
	return ""
}

// LivePublishURL 给出某个平台发布页的地址。
func (s *TopicService) LivePublishURL(platform string) string {
	if p, ok := publish.Get(platform); ok {
		return p.PublishURL
	}
	return ""
}

// liveView 组装当前状态。
func (s *TopicService) liveView(ps *publishState, platform string) (LiveView, error) {
	v := LiveView{Platform: platform}
	if platform == "" {
		return v, nil
	}
	url, err := ps.eng.LiveLocation(platform)
	if err != nil {
		// 会话还没起：能告诉界面「没开」，但不能把它当成错误弹出来
		return v, nil
	}
	v.Open = true
	v.URL = url
	v.Authed = ps.eng.Authed(platform)
	if w, h := ps.eng.LiveViewport(); w > 0 {
		v.W, v.H = w, h
	}
	s.liveWin.mu.Lock()
	v.Detached = s.liveWin.win != nil
	s.liveWin.mu.Unlock()
	return v, nil
}

/* ---------- 内部的接线活 ---------- */

// attachLiveFrames 把引擎的画面接到界面上。
//
// 在 publisher() 建引擎时调一次。用 Wails 事件而不是绑定调用：
// 绑定调用是「前端问一次、Go 答一次」，画面是「Go 主动推、前端收到就画」，
// 方向反了，用事件才对得上。
func (s *TopicService) attachLiveFrames(eng *publish.Engine, gate *liveGate) {
	eng.SetFrameHandler(func(f publish.Frame) {
		if s.app == nil {
			return
		}
		if !gate.allow() {
			return
		}
		s.app.Event.Emit("live:frame", f)
	})
}
