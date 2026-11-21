package main

import (
	"log"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"rongsoft/miniwork/app/internal/store"
	"rongsoft/miniwork/app/internal/tray"
)

// appName 是产品名。托盘菜单、窗口标题、日志都用它，免得三处写成三个样。
const appName = "Miniwork"

// shell 管桌面外壳：菜单栏图标、关窗行为、退出。
//
// 为什么需要它：定时刷新靠进程里的 ticker 推，进程不在就不刷。
// 用户点红叉，如果直接退出进程，第二天打开看到「昨天没跑」——
// 那不是定时器算错了，是他把「关窗」当成了「收起来」。
// 所以关窗之后进程要留着，那就必须有个地方能把它叫回来，也就是菜单栏。
//
// 反过来也要能真退出。托盘菜单里那一项是三处入口（红叉、Cmd+Q、托盘退出）
// 里唯一明确表达「我要它停」的地方，所以它必须是真退出，不能被自己的钩子拦住。
type shell struct {
	app *application.App
	svc *TopicService
	win *application.WebviewWindow

	trayOnce sync.Once
	systray  *application.SystemTray

	// menuResident 是「驻留」勾选项，托盘里改了状态要同步勾，否则菜单显示的和实际行为对不上。
	menuResident *application.MenuItem
	menuRefresh  *application.MenuItem

	// busy 防连点：刷新跑着的时候菜单项置灰，避免用户以为没反应又点两下。
	busy atomic.Bool

	// trayShown 记录托盘此刻是否真的可见。
	//
	// 用它做关窗的兜底：有些 Linux 桌面环境根本没托盘（缺扩展），
	// 那种情况下如果照样拦关窗，用户会得到「窗口没了、图标也没有」的僵局，
	// 除了活动监视器没有任何办法退出。所以托盘不可见时就不拦，让它正常退出。
	trayShown atomic.Bool

	mu       sync.Mutex
	resident bool // 关窗后是否驻留（而不是退出）

	quitting atomic.Bool
}

func newShell(app *application.App, svc *TopicService) *shell {
	return &shell{app: app, svc: svc}
}

// start 装配外壳。hidden 为真表示这次是开机自启拉起来的，窗口先不显示。
func (sh *shell) start(win *application.WebviewWindow, hidden bool) {
	sh.win = win

	// 默认驻留：键不存在就是「没被关掉」。见 store.SettingTrayDisabled 的注释。
	sh.resident = sh.svc.st.GetSetting(store.SettingTrayDisabled) != "1"

	// 自启进来的时候一定要有托盘，否则进程在跑、窗口不显示、菜单栏里也没有，
	// 用户除了活动监视器没有任何办法关掉它。这次运行期强制显示。
	if hidden && !sh.resident {
		sh.resident = true
		// 一并落库。只改内存的话，设置页读到的内存是「开」，下次启动读到的库是「关」，
		// 用户会看到开关自己跳回去，以为设置没保存住。
		// 正常情况下走不到这里：设置页开自启时会连带打开驻留，见 SetAutoStart。
		if err := sh.svc.st.SetSetting(store.SettingTrayDisabled, "0"); err != nil {
			log.Printf("记录托盘设置失败: %v", err)
		}
	}

	sh.watchClose()
	if sh.resident {
		// 直接调「主线程版本」而不是 showTray：这时候 app.Run() 还没开始，
		// Wails 的 impl 是 nil，走 ui() 派发会 panic。start 本来就在主线程上，
		// 不需要派发。
		sh.showTrayOnUI()
	}
}

// closeAction 是「点红叉时该干什么」。
type closeAction int

const (
	// closeDestroy 交给 Wails 的内置流程销毁窗口。窗口真被销毁之后，
	// 应用会按「最后一个窗口关闭」的规则退出——前提是别把那条规则关掉。
	closeDestroy closeAction = iota
	// closeHide 拦下这一下，只把窗口藏起来，进程留在菜单栏继续跑定时器。
	closeHide
)

// onClose 判断这一次关窗该销毁还是该藏起来。抽成独立函数是为了能测：
// 两条分支各自对应一个真实的坑——判错了要么关不掉程序（留下无窗口无图标的僵尸），
// 要么点红叉把进程一起带走，定时刷新静悄悄停掉。
func (sh *shell) onClose() closeAction {
	if sh.quitting.Load() {
		return closeDestroy // 正在退出，别再拦
	}
	if !sh.isResident() {
		return closeDestroy // 用户把驻留关了，关窗就是退出
	}
	if !sh.trayShown.Load() {
		// 说好要驻留，但菜单栏里其实没有图标。这时候拦住关窗等于把用户锁死，
		// 宁可违背一次「驻留」的承诺，也不能让他找不到退出的门。
		log.Printf("托盘不可见，关窗按退出处理")
		return closeDestroy
	}
	return closeHide
}

// watchClose 决定点红叉之后发生什么。
//
// 用 RegisterHook 而不是 OnWindowEvent：hook 同步执行，而且事件被 Cancel 之后
// 窗口自己注册的那个「销毁窗口」监听器就不会跑了。用 OnWindowEvent 拦不住，
// 它只会和内置监听器一起排队，窗口照样被销毁。
func (sh *shell) watchClose() {
	sh.win.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		if sh.onClose() != closeHide {
			return
		}
		e.Cancel()
		sh.win.Hide()
	})
}

func (sh *shell) isResident() bool {
	sh.mu.Lock()
	defer sh.mu.Unlock()
	return sh.resident
}

// trayPlace 是「图标待着的那个地方」在各平台的名字。
//
// macOS 叫菜单栏（屏幕顶部），Windows 叫托盘（右下角）。文案写死成「菜单栏」
// 的话，Windows 用户看到的是一个自己屏幕上不存在的东西。
func trayPlace() string {
	if runtime.GOOS == "darwin" {
		return "菜单栏"
	}
	return "托盘"
}

// ui 把碰原生控件的操作送回主线程。//
// 为什么需要它：SystemTray.Show/Hide、窗口的 Show/Hide/Focus 内部都走
// InvokeSync，从哪条协程调都安全；但菜单项的 SetLabel / SetChecked 是
// 直接调 AppKit 的，app.Show() 也是。而这些方法会被两个非主线程的调用方碰到：
// 设置页的开关（WebView 的 RPC 协程）和托盘「立即刷新」（我们起的 goroutine）。
// 从非主线程碰 NSMenuItem 属于「大部分时候看起来能用、偶尔崩得莫名其妙」那一类，
// 不值得赌。
//
// InvokeAsync 在当前已经位于主线程时会同步执行，所以主线程调用方
// （托盘菜单回调、关窗钩子）不会被推迟一帧。
//
// 有一条硬约束：**app.Run() 之前不能调这里**。这时候 Wails 的 impl 还是 nil，
// dispatchOnMainThread 第一行就解引用它，直接 panic（踩过）。
// 启动期要动托盘，走 showTrayOnUI 这类「已经在主线程上」的版本。
//
// 加 recover 是因为还有一条绕不开的路径：单实例回调。第二个实例启动时
// 会通知第一个实例「把窗口叫出来」，那一刻如果第一实例恰好还在启动过程中，
// 这里就会撞上上面那条约束。一个「叫窗口」的附带请求不该把整程序带走，
// 失败了最多是窗口没弹到前面。
func ui(fn func()) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("派发到主线程失败（可能尚未启动完成）: %v", r)
		}
	}()
	application.InvokeAsync(fn)
}

// setResident 切换「关窗后驻留」，同时落库、同步托盘勾选、通知界面。
func (sh *shell) setResident(on bool) {
	sh.mu.Lock()
	changed := sh.resident != on
	sh.resident = on
	sh.mu.Unlock()

	if !changed {
		return
	}
	if err := sh.svc.st.SetSetting(store.SettingTrayDisabled, boolStr(!on)); err != nil {
		log.Printf("记录托盘设置失败，只在本次运行生效: %v", err)
	}

	ui(func() {
		if sh.menuResident != nil {
			sh.menuResident.SetChecked(on)
		}
		if on {
			sh.showTrayOnUI()
		} else {
			sh.hideTrayOnUI()
		}
	})
	if sh.app != nil {
		sh.app.Event.Emit("shell:changed", map[string]any{"resident": on})
	}
}

func (sh *shell) showTray() { ui(sh.showTrayOnUI) }

// showTrayOnUI 只在主线程上跑（托盘图标的创建和菜单项构造都在这里）。
func (sh *shell) showTrayOnUI() {
	sh.trayOnce.Do(sh.buildTray)
	if sh.systray != nil {
		sh.systray.Show()
		sh.trayShown.Store(true)
	}
}

// hideTray 收起菜单栏图标。
//
// 收起来的同时必须保证用户还有入口：如果窗口此刻是藏着的，
// 托盘一收他就既看不见窗口也看不见图标，进程变成没人能管的后台。
func (sh *shell) hideTray() { ui(sh.hideTrayOnUI) }

func (sh *shell) hideTrayOnUI() {
	if sh.systray != nil {
		sh.systray.Hide()
	}
	sh.trayShown.Store(false)
	if sh.win != nil && !sh.win.IsVisible() {
		sh.showWindowOnUI()
	}
}

func (sh *shell) buildTray() {
	t := sh.app.SystemTray.New()
	t.SetTooltip(appName + " · 每天长出来的选题")

	// macOS 用 template 图，系统按菜单栏深浅色自动反色。
	//
	// Windows / Linux 没有 template 这回事，但 Wails 支持一对「浅色主题图 +
	// 深色主题图」，由它跟着系统主题切换。所以两套都得给：只给一套的话，
	// 碰上相反的主题就是白底白线（Win11 默认浅色任务栏，早先只给了浅色墨，
	// 打开就是一格空白）。Linux 那边 setDarkModeIcon 是空实现，多传一份不报错。
	if runtime.GOOS == "darwin" {
		if icon, err := tray.Template(); err == nil {
			t.SetTemplateIcon(icon)
		} else {
			log.Printf("托盘图标生成失败，用文字代替: %v", err)
			t.SetLabel("M")
		}
	} else {
		if icon, err := tray.DeskLight(); err == nil {
			t.SetIcon(icon)
		} else {
			log.Printf("托盘图标生成失败，用文字代替: %v", err)
			t.SetLabel("M")
		}
		if icon, err := tray.DeskDark(); err == nil {
			t.SetDarkModeIcon(icon)
		}
	}

	menu := application.NewMenu()

	open := menu.Add("打开主窗口")
	open.OnClick(func(*application.Context) { sh.showWindow() })

	sh.menuRefresh = menu.Add("立即刷新")
	sh.menuRefresh.OnClick(func(*application.Context) { go sh.refreshNow() })

	menu.AddSeparator()

	sh.menuResident = menu.AddCheckbox("关窗后驻留"+trayPlace(), sh.isResident())
	// 自己取反而不是读菜单的勾选状态：勾选由谁翻转各平台不一致，
	// 以内存里的 resident 为准，再把菜单同步成一致，不会越点越偏。
	sh.menuResident.OnClick(func(*application.Context) {
		sh.setResident(!sh.isResident())
	})

	menu.AddSeparator()

	quit := menu.Add("退出 " + appName)
	quit.OnClick(func(*application.Context) { sh.quit() })

	t.SetMenu(menu)
	// 左键直接开窗口，不用先弹菜单——这个图标最常见的用途就是「把它叫回来」。
	t.OnClick(func() { sh.showWindow() })

	sh.systray = t
}

func (sh *shell) showWindow() { ui(sh.showWindowOnUI) }

func (sh *shell) showWindowOnUI() {
	if sh.win == nil {
		return
	}
	sh.win.Show()
	sh.win.Focus()
	// macOS 上窗口显示出来还不够，应用可能还在别的应用后面，
	// 这里把整个应用提到前台，用户点了托盘才真的看得见窗口。
	if runtime.GOOS == "darwin" {
		sh.app.Show()
	}
}

// refreshNow 是托盘里的「立即刷新」。和界面上的刷新按钮走同一条路，
// 差别只是它在后台跑、不依赖窗口是否打开。
func (sh *shell) refreshNow() {
	if !sh.busy.CompareAndSwap(false, true) {
		return
	}
	defer sh.busy.Store(false)

	sh.setRefreshItem("正在刷新…", false)
	defer sh.setRefreshItem("立即刷新", true)

	if _, err := sh.svc.Run(sh.svc.currentNiche(), ""); err != nil {
		log.Printf("托盘刷新失败: %v", err)
		return
	}
	log.Printf("托盘刷新完成")
}

// setRefreshItem 改托盘菜单里那一项的文案和可用状态。
// 菜单项不是线程安全的（直接调 NSMenuItem），所以经由 ui 回主线程。
func (sh *shell) setRefreshItem(label string, enabled bool) {
	ui(func() {
		if sh.menuRefresh == nil {
			return
		}
		sh.menuRefresh.SetLabel(label)
		sh.menuRefresh.SetEnabled(enabled)
	})
}

// quit 是真退出。先立旗子再交给内置流程，否则会被上面那个钩子拦成「又藏起来了」。
func (sh *shell) quit() {
	sh.quitting.Store(true)
	sh.app.Quit()
}
