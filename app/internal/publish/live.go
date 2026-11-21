package publish

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// 把平台网页画进应用自己的窗口里。
//
// 为什么不干脆用系统的 WebView 承载网页：小红书、抖音的图片和视频上传
// 靠的是 CDP 的 setFileInputFiles——直接把文件塞进 input[type=file]。
// 系统 WebView 没有这个能力，网页里点「上传」只会弹出系统选择框，
// 「自动发布」这条路当场断掉。所以内核仍然是 Chromium。
//
// 那为什么不用 Chromium 的窗口：用户看到的是「点个按钮弹出一个 Chrome」，
// 观感像脚本，不像产品。折中办法是把 Chromium 的窗口挪到屏幕外，
// 用 CDP 的 screencast 持续把画面推回应用，用户的点击、滚轮、输入
// 再反向投递回去。网页是真的在渲染、真的在交互，只是它出现在我们的界面里。
//
// 代价是画面有几十毫秒延迟、快速滚动时略糊。换来的是「自动化一步没少」。

// Frame 是一帧画面。Data 是 base64 JPEG，直接塞进 <img src>。
type Frame struct {
	Data string `json:"data"`
	// W / H 是视口尺寸（CSS 像素）。界面按它算点击坐标，别用图片像素。
	W int `json:"w"`
	H int `json:"h"`
}

// LiveInput 是一次用户操作。
//
// 只收这几种：这些平台的页面里真正需要的操作就是「点一下」「滚一下」
// 「填几个字」。故意不做键盘全键位模拟——中文输入的组合过程走 IME，
// 反投过去必然丢字；真要改文案，左边编辑器里改比在网页里敲更靠得住。
type LiveInput struct {
	// Kind: click / move / wheel / text / key / scroll
	Kind string  `json:"kind"`
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
	DX   float64 `json:"dx"`
	DY   float64 `json:"dy"`
	Text string  `json:"text"`
	Key  string  `json:"key"`
}

// 视口尺寸的上下界。
//
// 上限不是随便定的：screencast 每帧都要 base64 后过界面通道，
// 视口开到 3000 宽时一帧能到 800KB，前端每秒收十几帧就卡死了。
const (
	liveMinW = 360
	liveMinH = 300
	liveMaxW = 1920
	liveMaxH = 1400
)

// liveState 是「当前正在投屏的那一屏」。
//
// 单独一把锁，不和 Engine.mu 混用：Engine.mu 管的是浏览器会话，
// 而这里管的是推帧 goroutine 和界面之间的握手。两件事的持有时间差着
// 两个数量级，混在一起会让 Authed 这种本该瞬间返回的调用排在推帧后面。
type liveState struct {
	mu   sync.Mutex
	seq  int
	emit func(Frame)
	stop func()
	w, h int
	// dsf 是界面那块屏的缩放比（Windows 125% 就是 1.25）。
	// 视口按 CSS 像素排，画面却要按物理像素出，否则在 125% 的屏上
	// 一张 900px 宽的 JPEG 铺到 900 CSS 像素里就是被放大 1.25 倍，
	// 字全是毛边——用户的原话是「字体失真了」。
	dsf  float64
	busy bool
}

// SetDeviceScale 告诉引擎界面所在屏幕的缩放比。
//
// 不做自适应跟踪（拖到另一块屏会自动变），因为这里的代价看得很清楚：
// 缩放比一变就要重设视口、重开推帧，画面会黑一下。拖窗口换屏幕不常见，
// 不值得为它让每次打开都冒一次黑屏的风险。
func (e *Engine) SetDeviceScale(f float64) {
	if f < 1 {
		f = 1
	}
	if f > 3 {
		f = 3
	}
	e.lm.mu.Lock()
	e.lm.dsf = f
	e.lm.mu.Unlock()
}

// SetFrameHandler 由界面侧注入画面接收者。
func (e *Engine) SetFrameHandler(fn func(Frame)) {
	e.lm.mu.Lock()
	e.lm.emit = fn
	e.lm.mu.Unlock()
}

// LiveOpen 开始把某个平台的页面投到界面上。
//
// url 为空时只接上画面、不导航——用户可能想留在上次那一页。
func (e *Engine) LiveOpen(platform, url string) error {
	p, ok := Get(platform)
	if !ok {
		return fmt.Errorf("未知平台: %q", platform)
	}
	ctx, err := e.session(platform)
	if err != nil {
		return err
	}

	e.lm.mu.Lock()
	e.lm.seq++
	seq := e.lm.seq
	emit := e.lm.emit
	w, h := e.lm.w, e.lm.h
	e.lm.busy = true
	if e.lm.stop != nil {
		e.lm.stop()
		e.lm.stop = nil
	}
	e.lm.mu.Unlock()

	if emit == nil {
		return errors.New("界面还没接上画面通道")
	}

	if w > 0 && h > 0 {
		if err := e.setViewport(ctx, w, h); err != nil {
			return err
		}
	}

	e.logf("正在把%s的页面接进来", p.Name)
	go e.pump(ctx, seq, emit, w, h)

	if url == "" {
		return nil
	}
	if err := chromedp.Run(ctx, chromedp.Navigate(url)); err != nil {
		return startError(err)
	}
	return nil
}

// LiveClose 停止投屏（浏览器进程留着，下次接上更快）。
func (e *Engine) LiveClose() {
	e.lm.mu.Lock()
	e.lm.seq++
	stop := e.lm.stop
	e.lm.stop = nil
	e.lm.busy = false
	e.lm.mu.Unlock()
	if stop != nil {
		stop()
	}
	if ctx := e.currentCtx(); ctx != nil {
		_ = chromedp.Run(ctx, page.StopScreencast())
	}
}

// LiveNavigate 让页面跳到一个地址。
func (e *Engine) LiveNavigate(platform, url string) error {
	if _, ok := Get(platform); !ok {
		return fmt.Errorf("未知平台: %q", platform)
	}
	ctx, err := e.session(platform)
	if err != nil {
		return err
	}
	return chromedp.Run(ctx, chromedp.Navigate(url))
}

// LiveReload 刷新当前页。
func (e *Engine) LiveReload(platform string) error {
	ctx, err := e.session(platform)
	if err != nil {
		return err
	}
	return chromedp.Run(ctx, chromedp.Reload())
}

// LiveResize 把网页的视口改成界面那块区域的尺寸。
//
// 不做等比缩放：视口和显示区域一样大，界面上的坐标就是页面里的坐标，
// 反投时不用换算，也就不会出现「点偏了」这种最难查的 bug。
func (e *Engine) LiveResize(platform string, w, h int) error {
	w, h = clampViewport(w, h)
	e.lm.mu.Lock()
	e.lm.w, e.lm.h = w, h
	e.lm.mu.Unlock()

	ctx := e.currentCtxOf(platform)
	if ctx == nil {
		// 还没开过：记下尺寸，等 LiveOpen 时一次性设上
		return nil
	}
	return e.setViewport(ctx, w, h)
}

// LiveJS 在页面里跑一段脚本，用来做「滚到顶」「点某个按钮」这类杂事。
func (e *Engine) LiveJS(platform, script string) error {
	ctx, err := e.session(platform)
	if err != nil {
		return err
	}
	return chromedp.Run(ctx, chromedp.Evaluate(script, nil))
}

// LiveEval 在页面里算一个表达式并把结果读回来。
//
// 界面上暂时用不到，但它是「投屏到底通不通」唯一可靠的自证方式：
// 点一下画面，看页面里的状态变没变。测试和排查都靠它。
//
// 结果按文本回：表达式可能返回布尔、数字、字符串，这里一律转成字，
// 免得「查个 bool 就被反序列化错误打断」——排查时最不需要的就是这种噪音。
func (e *Engine) LiveEval(platform, expr string) (string, error) {
	ctx := e.currentCtxOf(platform)
	if ctx == nil {
		return "", ErrNotLoggedIn
	}
	var raw any
	if err := chromedp.Run(ctx, chromedp.Evaluate(expr, &raw)); err != nil {
		return "", err
	}
	if raw == nil {
		return "", nil
	}
	if s, ok := raw.(string); ok {
		return s, nil
	}
	return fmt.Sprint(raw), nil
}

// LiveLocation 返回页面当前地址。界面拿它显示「现在停在哪一页」。
//
// 单独给一个方法而不是让界面去调 JS：地址是判断「发布到底成没成」的依据
// （还停在 /publish/publish 就说明没发出去），这种判断不该依赖前端脚本。
func (e *Engine) LiveLocation(platform string) (string, error) {
	ctx := e.currentCtxOf(platform)
	if ctx == nil {
		return "", ErrNotLoggedIn
	}
	var out string
	if err := chromedp.Run(ctx, chromedp.Location(&out)); err != nil {
		return "", err
	}
	return out, nil
}

// LiveViewport 返回上一次设定的视口尺寸，没设过就是 0。
func (e *Engine) LiveViewport() (int, int) {
	e.lm.mu.Lock()
	defer e.lm.mu.Unlock()
	return e.lm.w, e.lm.h
}

// LiveBack 回上一页。
//
// 用 history.back() 而不是自己记地址栈：页面里多半是 SPA，
// 平台的「后退」行为和我们记的地址对不上，让它自己退才不会跳到空白页。
func (e *Engine) LiveBack(platform string) error {
	ctx := e.currentCtxOf(platform)
	if ctx == nil {
		return ErrNotLoggedIn
	}
	return chromedp.Run(ctx, chromedp.Evaluate(`history.back()`, nil))
}

// LiveInput 把一次操作投回页面。
func (e *Engine) LiveInput(platform string, ev LiveInput) error {
	ctx := e.currentCtxOf(platform)
	if ctx == nil {
		return ErrNotLoggedIn
	}
	return doInput(ctx, ev)
}

/* ---------- 内部 ---------- */

// session 拿一个属于 platform 的会话，必要时启动浏览器。
func (e *Engine) session(platform string) (context.Context, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.ensure(platform)
}

// currentCtx 返回活着的会话上下文，没有就 nil。
func (e *Engine) currentCtx() context.Context {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.ctx == nil || e.ctx.Err() != nil {
		return nil
	}
	return e.ctx
}

// currentCtxOf 只在会话正好属于 platform 时返回它，避免把点击投到别的平台页面上。
func (e *Engine) currentCtxOf(platform string) context.Context {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.ctx == nil || e.ctx.Err() != nil || e.plat != platform {
		return nil
	}
	return e.ctx
}

func clampViewport(w, h int) (int, int) {
	if w < liveMinW {
		w = liveMinW
	}
	if h < liveMinH {
		h = liveMinH
	}
	if w > liveMaxW {
		w = liveMaxW
	}
	if h > liveMaxH {
		h = liveMaxH
	}
	return w, h
}

// setViewport 让页面按界面上那块区域的尺寸排版。
//
// 宽高按 CSS 像素给（界面上的坐标就是页面里的坐标，反投时不用换算），
// 缩放比按屏幕给：deviceScaleFactor 设成屏幕的缩放比，Chromium 就会
// 按物理像素渲染，出来的 JPEG 正好一像素对一像素。
// 上一版这里写死 1，于是在 125%/150% 的 Windows 上，画面被系统放大，
// 一屏幕的字全是虚的。
func (e *Engine) setViewport(ctx context.Context, w, h int) error {
	w, h = clampViewport(w, h)
	e.lm.mu.Lock()
	dsf := e.lm.dsf
	e.lm.mu.Unlock()
	if dsf < 1 {
		dsf = 1
	}
	return chromedp.Run(ctx, emulation.SetDeviceMetricsOverride(int64(w), int64(h), dsf, false))
}

// pump 收帧、转给界面。
//
// chromedp 的 screencast 是「ack 一帧才发一帧」，所以推送速度由这里决定：
// 界面越慢，画面越卡。队列只留两帧，宁可丢旧帧也不能让 CDP 的读循环堵住——
// 读循环一堵，Agent 的事件、导航响应全都停，表现成「点了没反应」。
func (e *Engine) pump(ctx context.Context, seq int, emit func(Frame), cssW, cssH int) {
	frames := make(chan *page.EventScreencastFrame, 2)

	// 这个 chromedp 版本的 ListenTarget 不返回取消函数，取消靠上下文：
	// 给监听器一个可取消的子 ctx，停的时候 cancel 它。
	lctx, cancelListen := context.WithCancel(ctx)
	chromedp.ListenTarget(lctx, func(ev any) {
		f, ok := ev.(*page.EventScreencastFrame)
		if !ok {
			return
		}
		// ack 必须扔到别的 goroutine：这个回调跑在 CDP 读循环里，
		// 在里面同步 Run 一条命令会自己等自己。
		go func() {
			_ = chromedp.Run(ctx, page.ScreencastFrameAck(f.SessionID))
		}()
		select {
		case frames <- f:
		default:
			select {
			case <-frames:
			default:
			}
			select {
			case frames <- f:
			default:
			}
		}
	})

	e.lm.mu.Lock()
	if e.lm.seq != seq {
		e.lm.mu.Unlock()
		cancelListen()
		return
	}
	e.lm.stop = cancelListen
	e.lm.mu.Unlock()

	// 内置浏览器是 Chrome for Testing，ack 之前必须先 start；
	// 参数取 jpeg 是因为 png 一帧能有 2MB，过不了界面通道。
	err := chromedp.Run(ctx, page.StartScreencast().
		WithFormat(page.ScreencastFormatJpeg).
		// 78 是给「看个大概」用的，字边缘会有一圈 JPEG 的蚊香纹，
		// 中文小字在这个质量下最明显。90 加上按物理像素出图，
		// 正文才像正文，不像截图。
		WithQuality(90).
		WithMaxWidth(2560).
		WithMaxHeight(1800).
		WithEveryNthFrame(1))
	if err != nil {
		e.logf("画面接不上：%v", err)
		return
	}
	defer chromedp.Run(ctx, page.StopScreencast())

	for {
		select {
		case <-ctx.Done():
			return
		case f, ok := <-frames:
			if !ok {
				return
			}
			e.lm.mu.Lock()
			alive := e.lm.seq == seq
			e.lm.mu.Unlock()
			if !alive {
				return
			}
			// 尺寸一律用我们设进去的 CSS 视口，不用 metadata：
			// 界面拿这个尺寸反算点击坐标，metadata 报的是设备像素，
			// 缩放比一上 1.5，点哪都偏三分之一屏。
			w, h := cssW, cssH
			if w <= 0 || h <= 0 {
				if f.Metadata != nil {
					w = int(f.Metadata.DeviceWidth)
					h = int(f.Metadata.DeviceHeight)
				}
			}
			emit(Frame{Data: f.Data, W: w, H: h})
		}
	}
}

// doInput 把一次操作翻译成 CDP 的输入事件。
func doInput(ctx context.Context, ev LiveInput) error {
	x, y := ev.X, ev.Y
	switch strings.ToLower(strings.TrimSpace(ev.Kind)) {
	case "click":
		return chromedp.Run(ctx,
			clickAt(x, y, 1, 1),
			clickAt(x, y, 1, 0),
			// 让页面知道鼠标停在这儿：很多平台的下拉菜单靠 hover 展开
			chromedp.ActionFunc(func(ctx context.Context) error {
				return input.DispatchMouseEvent(input.MouseMoved, x, y).Do(ctx)
			}),
		)
	case "move":
		return chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
			return input.DispatchMouseEvent(input.MouseMoved, x, y).Do(ctx)
		}))
	case "wheel", "scroll":
		dy := ev.DY
		if dy == 0 && ev.DX == 0 {
			return nil
		}
		return chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
			return input.DispatchMouseEvent(input.MouseWheel, x, y).
				WithDeltaX(ev.DX).WithDeltaY(dy).Do(ctx)
		}))
	case "text":
		if ev.Text == "" {
			return nil
		}
		return chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
			return input.InsertText(ev.Text).Do(ctx)
		}))
	case "key":
		return chromedp.Run(ctx, keyPress(ev.Key))
	default:
		return fmt.Errorf("不认识的操作: %q", ev.Kind)
	}
}

// clickAt 发一次鼠标按下或抬起。
func clickAt(x, y float64, count int64, down int64) chromedp.Action {
	kind := input.MouseReleased
	buttons := int64(0)
	if down == 1 {
		kind = input.MousePressed
		buttons = 1
	}
	return chromedp.ActionFunc(func(ctx context.Context) error {
		return input.DispatchMouseEvent(kind, x, y).
			WithButton(input.Left).
			WithClickCount(count).
			WithButtons(buttons).
			Do(ctx)
	})
}

// keyPress 处理少数几个「非它不可」的按键。
//
// 只做退格、回车、Tab、Esc：这四件事在平台页面上真的会用到
// （清空输入框、确认联想词、切换焦点、关掉弹窗）。全键位映射
// 会把中文输入法搅成一团乱码，不值得。
func keyPress(name string) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		var code, key, text string
		var vk int64
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "enter":
			code, key, text, vk = "Enter", "Enter", "\r", 13
		case "backspace":
			code, key, vk = "Backspace", "Backspace", 8
		case "tab":
			code, key, vk = "Tab", "Tab", 9
		case "escape", "esc":
			code, key, vk = "Escape", "Escape", 27
		default:
			return fmt.Errorf("不支持的按键: %q", name)
		}
		down := input.DispatchKeyEvent(input.KeyDown).
			WithKey(key).WithCode(code).
			WithWindowsVirtualKeyCode(vk).WithNativeVirtualKeyCode(vk)
		if text != "" {
			down = down.WithText(text).WithUnmodifiedText(text)
		}
		if err := down.Do(ctx); err != nil {
			return err
		}
		return input.DispatchKeyEvent(input.KeyUp).
			WithKey(key).WithCode(code).
			WithWindowsVirtualKeyCode(vk).WithNativeVirtualKeyCode(vk).
			Do(ctx)
	})
}
