package publish

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	_ "modernc.org/sqlite" // 只读打开 Chrome 的 Cookies 库判断登录过没有
)

// ErrNoBrowser 表示找不到可驱动的浏览器。
//
// 内置之后这个错误理论上不该出现——它现在只意味着安装包不完整
// （有人删了包里的 browser 目录），或者机器上一份浏览器都没有。
// 单独一个哨兵错误是为了界面能给出「重装应用」这种具体指引，
// 而不是把 os.Stat 的报错原样丢给用户。
var ErrNoBrowser = errors.New("找不到可用的浏览器：安装包里自带的那份不在了，系统里也没装 Chrome 或 Edge")

// ErrNotLoggedIn 表示这个平台还没登录。
var ErrNotLoggedIn = errors.New("这个平台还没登录")

// Logger 收进度文字。发布一次要几十秒，中间必须让用户看见在干什么。
type Logger func(string)

// Options 是引擎的配置。
type Options struct {
	// DataDir 是浏览器登录态的根目录。每个平台一个子目录，
	// 相当于每个平台一个独立浏览器——登录态互不干扰，也不会碰到
	// 用户自己日常用的那个 Chrome 配置。
	DataDir string
	// ExecPath 只在测试里用：指定一个浏览器，跳过自动查找。
	ExecPath string
	// Headless 留着是为了测试里显式要无头。正式运行本来就是无头，见 Windowed。
	Headless bool
	// Windowed 切回「有头 + 窗口挪到屏幕外」。
	//
	// 只用来排查问题，比如怀疑某个平台在无头下行为不一致。
	// 平时不要开：那个窗口会抢走 macOS 的前台焦点，
	// 用户看到菜单栏变成「Google Chrome for Testing」，比弹窗更吓人。
	Windowed bool
	// NoSandbox 关掉浏览器的沙箱。
	//
	// 默认关着：多一层隔离总是好的。但 macOS 上必须打开——见 ensure 里的注释。
	// 另外有些环境（容器、被策略限制的机器）里 Chrome 的沙箱起不来，
	// 表现是渲染进程一路 crash，报出来的却只有一句 context canceled。
	// 留这个开关是为了那种机器还能用。
	NoSandbox bool
	Logger    Logger
}

// Engine 管一台浏览器。
//
// 同一时刻只允许一个发布任务：两个任务抢同一个 profile 目录，
// Chrome 会把第二个当成「已有实例」直接退出，第二个任务就莫名其妙失败。
type Engine struct {
	opts Options

	mu     sync.Mutex
	ctx    context.Context
	cancel context.CancelFunc
	plat   string // 当前会话属于哪个平台
	exe    string

	// lm 管「投屏」那套状态：画面往哪推、视口多大、上一轮推帧停没停。
	// 和 mu 分开是因为推帧是每秒十几帧的热路径，而 mu 下面挂着启动浏览器
	// 这种几百毫秒的操作，混在一起会互相拖。
	lm liveState

	// wxToken 记着「公众号后台的基本配置页，已经为哪个 token 翻过了」。
	//
	// 界面每两秒轮询一次登录状态，若每次都导航到那一页，右侧面板会闪成
	// 一片、用户想点「重置」都点不到。翻一次就够，翻过就只读不跳。
	wmu     sync.Mutex
	wxToken string
}

// New 建一个引擎。不会启动浏览器，第一次用到才启动。
func New(opts Options) *Engine {
	if opts.Logger == nil {
		opts.Logger = func(string) {}
	}
	return &Engine{opts: opts, exe: opts.ExecPath}
}

// BrowserPath 返回要驱动的浏览器路径。
//
// 顺序：内置 > 本机 Chrome > 本机 Edge。
//
// 内置排第一，因为它是唯一我们保证得了存在性和版本的一份——用户机器上
// 装没装 Chrome、装的是不是被策略锁住的版本，我们控制不了。用户自己装的
// 那份留作兜底：内置目录被误删、或杀毒软件把 Chromium 当可疑程序清掉，
// 还发得出去。
func (e *Engine) BrowserPath() (string, error) {
	if e.exe != "" {
		if _, err := os.Stat(e.exe); err == nil {
			return e.exe, nil
		}
	}
	for _, p := range browserCandidates() {
		if _, err := os.Stat(p); err == nil {
			e.exe = p
			return p, nil
		}
	}
	return "", ErrNoBrowser
}

// Available 告诉界面「这台机器能不能发布」。
func (e *Engine) Available() bool {
	_, err := e.BrowserPath()
	return err == nil
}

// browserCandidates 按优先级列出可以驱动的浏览器。
//
// 内置的排在系统浏览器前面，理由见 BrowserPath。
func browserCandidates() []string {
	var out []string
	if p, _ := BundledBrowser(); p != "" {
		out = append(out, p)
	}
	return append(out, systemBrowsers()...)
}

// systemBrowsers 是用户自己装的那几个。
func systemBrowsers() []string {
	switch runtime.GOOS {
	case "windows":
		var out []string
		for _, base := range []string{
			os.Getenv("ProgramFiles"),
			os.Getenv("ProgramFiles(x86)"),
			os.Getenv("LOCALAPPDATA"),
		} {
			if base == "" {
				continue
			}
			out = append(out,
				filepath.Join(base, `Google\Chrome\Application\chrome.exe`),
				filepath.Join(base, `Microsoft\Edge\Application\msedge.exe`),
			)
		}
		return out
	case "darwin":
		return []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
			filepath.Join(os.Getenv("HOME"), "Applications/Google Chrome.app/Contents/MacOS/Google Chrome"),
		}
	default:
		return []string{
			"/usr/bin/google-chrome",
			"/usr/bin/chromium",
			"/usr/bin/microsoft-edge",
		}
	}
}

// profileDir 是某个平台的登录态目录。
func (e *Engine) profileDir(platform string) string {
	return filepath.Join(e.opts.DataDir, "browser", platform)
}

// needsNoSandbox 说明 macOS 上为什么要关沙箱。
//
// macOS 的 Chrome 沙箱靠「父进程通过 sandbox extension 把权限传给子进程」，
// 而这个 extension 只对经由 LaunchServices 启动的进程有效。我们（以及
// Playwright、puppeteer、selenium）都是从普通进程直接 exec 起来的，
// 于是子进程一路报 sandbox initialization failed、网络服务和渲染进程
// 反复崩溃，主进程还活着，CDP 侧只看到一句 context canceled——
// 排查起来毫无线索。
//
// Playwright 在 macOS 上同样默认加 --no-sandbox，不是偷懒。
// 风险可接受：这里只打开我们指定的几个平台页面，不加载用户给的 URL。
func needsNoSandbox() bool {
	return runtime.GOOS == "darwin"
}

// ensure 保证有一个属于 platform 的会话。
//
// 换平台时先关掉旧会话：不同平台的登录态在各自 profile 里，
// 一个浏览器进程只能用一个 profile。
func (e *Engine) ensure(platform string) (context.Context, error) {
	if e.ctx != nil && e.plat == platform && e.ctx.Err() == nil {
		return e.ctx, nil
	}
	e.shutdown()

	exe, err := e.BrowserPath()
	if err != nil {
		return nil, err
	}
	dir := e.profileDir(platform)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("创建登录态目录失败: %w", err)
	}

	// 先看有没有上次留下的窗口。应用重启、或者会话被回收之后，
	// 进程还在占着 profile，直接起新的会被 Chrome 拒绝（锁已存在）。
	// 能接管就接管：那个窗口可能正停在扫码页。
	if ctx, cancel, ok := adoptExisting(dir); ok {
		// 只接管「和我们这次要起的形态一致」的进程。
		// 上一次要是排查问题时开的有头窗口，它还坐在屏幕上；
		// 接管过来等于把那个窗口当成了自己的画面来源，用户会莫名其妙
		// 看到屏幕中间一个 Chrome。形态不一致就清掉重开。
		if readShape(dir) == e.shape() {
			e.ctx, e.plat, e.cancel = ctx, platform, cancel
			e.logf("接上了已经开着的%s页面", platformName(platform))
			return ctx, nil
		}
		cancel()
	}
	// 接管不了（进程已退出、或端口不通）：清掉残留的锁，否则新进程起不来。
	clearStaleLock(dir)

	opts := append([]chromedp.ExecAllocatorOption{},
		chromedp.DefaultExecAllocatorOptions[:]...)
	opts = append(opts,
		chromedp.ExecPath(exe),
		chromedp.Flag("user-data-dir", dir),
		chromedp.Flag("no-first-run", true),
		chromedp.Flag("no-default-browser-check", true),
		// 关掉「Chrome 正受自动化软件控制」那条提示条：
		// 它不是安全问题，只是让客户以为程序在做坏事。
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.Flag("disable-features", "Translate,AcceptCHFrame"),
		// 窗口/标签不可见时 Chromium 会把渲染进程降频甚至挂起。
		// 无头和离屏都属于「系统认为你没在看」，不关掉的话画面会卡住不动。
		chromedp.Flag("disable-backgrounding-occluded-windows", true),
		chromedp.Flag("disable-renderer-backgrounding", true),
		chromedp.Flag("disable-background-timer-throttling", true),
	)

	// 用新版无头跑。
	//
	// 早先是「有头 + 用户自己看窗口」，因为扫码、处理验证码都得有人。
	// 现在这些全搬到我们自己的界面里了（画面和输入都走 CDP，见 live.go），
	// 那个窗口只剩下坏处：一启动就抢走 macOS 的前台焦点，用户会看到
	// 菜单栏突然变成「Google Chrome for Testing」——比弹窗更让人心里没底。
	//
	// 用 --headless=new 而不是老的 --headless：新版无头从 Chrome 112 起
	// 和有头共用同一套渲染链路和 UA，站点那边看不出区别；老无头的 UA 里
	// 带着 HeadlessChrome，会被小红书一类站点直接判成机器人。
	if !e.opts.Windowed {
		opts = append(opts, chromedp.Flag("headless", "new"))
	} else {
		// 排查用：有窗口，但挪到所有屏幕之外，用户看不见。
		opts = append(opts,
			chromedp.Flag("app", "about:blank"),
			chromedp.Flag("window-size", "1280,900"),
			chromedp.Flag("window-position", "-4000,-4000"),
		)
	}
	if e.opts.NoSandbox || needsNoSandbox() {
		opts = append(opts, chromedp.Flag("no-sandbox", true))
	}

	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	ctx, ctxCancel := chromedp.NewContext(allocCtx)
	if err := chromedp.Run(ctx); err != nil {
		ctxCancel()
		allocCancel()
		return nil, startError(err)
	}

	e.ctx = ctx
	e.plat = platform
	e.cancel = func() { ctxCancel(); allocCancel() }
	// 记下这次是以什么形态起的，下次重启才知道能不能接管。
	writeShape(dir, e.shape())

	// 无头身份要抹干净，否则平台一眼看出来。
	//
	// --headless=new 号称和真的有头共用渲染链路，但 UA 里那条尾巴还在：
	//   Mozilla/5.0 ... HeadlessChrome/157.0.0.0 Safari/537.36
	// 小红书创作平台看到 HeadlessChrome 直接甩一张「安全限制 300012
	// 当前网络存在风险」——用户完全不知道自己哪里做错了，只会以为
	// 这软件坏了。所以改成真的 Chrome 再进页面。
	if err := fixHeadlessIdentity(ctx); err != nil {
		e.logf("抹无头标记失败（不影响打开页面，但平台可能要求验证）：%v", err)
	}

	e.logf("已启动 %s 的浏览器", platformName(platform))
	return ctx, nil
}

// fixHeadlessIdentity 把浏览器自己报的身份改成正常 Chrome。
//
// UA 从浏览器里读出来再替换，不写死版本号：写死的话每次随包升级浏览器
// 都要手工改一行，忘了就变成「UA 说 Chrome 120、实际是 157」——
// 那种不一致比明说无头更容易被判定成异常。
func fixHeadlessIdentity(ctx context.Context) error {
	var ua string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`navigator.userAgent`, &ua)); err != nil {
		return err
	}
	if !strings.Contains(ua, "Headless") {
		return nil
	}
	fixed := strings.ReplaceAll(ua, "HeadlessChrome", "Chrome")
	if err := chromedp.Run(ctx, emulation.SetUserAgentOverride(fixed)); err != nil {
		return err
	}
	// navigator.webdriver 在新版 Chrome 里已经是 false（靠 --disable-blink-features
	// 关掉的），但有些 SDK 会去读它、还有些读的是 chrome 对象在不在。
	// 这里只补最常被问的那一个，多改反而容易改出破绽。
	return chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		_, err := page.AddScriptToEvaluateOnNewDocument(
			`Object.defineProperty(navigator, 'webdriver', {get: () => undefined});`).Do(ctx)
		return err
	}))
}

// shape 是这次起的形态：headless 还是带窗口。
//
// 只用来判断「上次那个进程还能不能接管」。两种形态在用户眼里完全不一样
// （一个看不见，一个可能是屏幕中间一个 Chrome 窗口），混着接管会出事。
func (e *Engine) shape() string {
	if e.opts.Windowed {
		return "window"
	}
	return "headless"
}

// shapeFile 放在 profile 目录里。Chrome 不认识这个文件，也不会管它。
const shapeFile = "mw-shape"

func readShape(dir string) string {
	b, err := os.ReadFile(filepath.Join(dir, shapeFile))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func writeShape(dir, s string) {
	_ = os.WriteFile(filepath.Join(dir, shapeFile), []byte(s+"\n"), 0o644)
}

// shutdown 关掉当前会话。
func (e *Engine) shutdown() {
	if e.cancel != nil {
		e.cancel()
	}
	e.ctx, e.cancel, e.plat = nil, nil, ""
}

// Close 在退出应用时调用。
func (e *Engine) Close() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.shutdown()
}

func (e *Engine) logf(format string, args ...any) {
	e.opts.Logger(fmt.Sprintf(format, args...))
}

// OpenLogin 打开平台的登录页，把窗口留给用户扫码。
//
// 不做「自动填账号密码」：这些平台都要求扫码或短信，替用户保管密码
// 既做不到也不该做。这里只负责把门打开。
func (e *Engine) OpenLogin(platform string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	p, ok := Get(platform)
	if !ok {
		return fmt.Errorf("未知平台: %q", platform)
	}
	ctx, err := e.ensure(platform)
	if err != nil {
		return err
	}
	// 不说「请用手机扫码」：小红书创作平台现在只有手机号+验证码，
	// 页面上根本没有二维码。文案指着扫码，用户会去屏幕上找一个不存在的东西，
	// 结论就是「登录坏了」。让他照页面给的来。
	e.logf("正在打开%s登录页，画面就在窗口里，直接在上面登录（扫码或手机验证码，看页面给哪个）", p.Name)
	return chromedp.Run(ctx, chromedp.Navigate(p.LoginURL))
}

// Authed 判断某个平台当前是否已登录。
//
// 先看磁盘上的 profile，再问活着的浏览器会话。顺序是有讲究的：
//
// 用户扫完码，Chrome 立刻把 cookie 写进 profile 的 Cookies 库，而窗口
// 还开在那儿。如果先走 CDP 那条路，只要窗口状态稍有异常（用户关掉了、
// 换了标签、或者会话被引擎回收），就会答「没登录」——用户明明刚扫完，
// 界面还在让他再扫一次，这就是最气人的那种错。
//
// 磁盘这条路不受窗口开没开影响，所以排前面。
func (e *Engine) Authed(platform string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.authedLocked(platform)
}

// CurrentPlatform 返回引擎此刻开着的是哪个平台的会话，没开会话返回空串。
//
// 引擎只有一个会话（一个浏览器进程、一份 profile）：「公众号绑定」
// 和「正在发布小红书」同时来时，谁能用、谁要让，得先问一句。
// 不问就抢，表现是发布跑到一半页面跳走了。
func (e *Engine) CurrentPlatform() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.ctx == nil || e.ctx.Err() != nil {
		return ""
	}
	return e.plat
}

// authedLocked 是 Authed 的本体，调用方必须已持有 e.mu。
//
// 拆出来是因为抓数据那条路进来时已经握着锁：Engine 的会话
// （e.ctx / e.plat）不能并发改，抓取全程要独占。Go 的
// sync.Mutex 不可重入，直接在锁里调 Authed 会当场死锁。
func (e *Engine) authedLocked(platform string) bool {
	p, ok := Get(platform)
	if !ok {
		return false
	}
	if !e.Available() {
		return false
	}
	if e.cookieInProfile(platform, p.Cookie) {
		return true
	}
	// 磁盘上还没有：可能是刚登录、Chrome 还没刷盘。这时候问活的会话。
	if e.ctx == nil || e.plat != platform || e.ctx.Err() != nil {
		return false
	}
	ctx := e.ctx
	var cookies []*network.Cookie
	err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		var err error
		cookies, err = network.GetCookies().WithURLs([]string{p.PublishURL}).Do(ctx)
		return err
	}))
	if err != nil {
		return false
	}
	return hasCookie(cookies, p.Cookie)
}

// cookieInProfile 在没有会话时，去 profile 的 Cookies 库里找登录凭证。
//
// 这是纯文件检查，不启动浏览器——否则一进发布台就会弹出一个窗口。
// 库里 value 是加密的（Windows 走 DPAPI），这里只数「有没有这条 cookie」：
// 判断「登录过」够用，凭证是否还有效在真正发布时自然会见分晓。
func (e *Engine) cookieInProfile(platform, name string) bool {
	if name == "" {
		return false
	}
	base := e.profileDir(platform)
	cands := []string{
		filepath.Join(base, "Default", "Network", "Cookies"),
		filepath.Join(base, "Default", "Cookies"),
	}
	for _, db := range cands {
		if fi, err := os.Stat(db); err != nil || fi.Size() == 0 {
			continue
		}
		// immutable=1：浏览器开着时库是被锁住的，普通只读打开会报 database is locked。
		dsn := "file:" + db + "?immutable=1&mode=ro"
		conn, err := sql.Open("sqlite", dsn)
		if err != nil {
			continue
		}
		var n int
		err = conn.QueryRow(`SELECT count(*) FROM cookies WHERE name = ?`, name).Scan(&n)
		conn.Close()
		if err == nil && n > 0 {
			return true
		}
	}
	return false
}

func hasCookie(list []*network.Cookie, name string) bool {
	for _, c := range list {
		if c.Name == name && c.Value != "" {
			return true
		}
	}
	return false
}

// platformName 给日志用。
func platformName(key string) string {
	if p, ok := Get(key); ok {
		return p.Name
	}
	return key
}

// waitAny 等一组候选选择器里任意一个出现，返回命中的那个。
//
// 平台的前端框架是异步渲染的，navigate 完立刻找元素一定找不到。
// 候选列表是因为同一个输入框在不同版本里叫不同名字，写死一个必然过期。
func waitAny(ctx context.Context, sels []string, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for {
		if s, err := pick(ctx, sels); err == nil && s != "" {
			return s, nil
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("等了 %s 也没找到（试过 %s）", timeout, strings.Join(sels, " / "))
		}
		time.Sleep(400 * time.Millisecond)
	}
}
