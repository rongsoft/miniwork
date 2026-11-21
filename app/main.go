package main

import (
	"embed"
	"log"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v3/pkg/application"

	"rongsoft/miniwork/app/internal/ai"
	"rongsoft/miniwork/app/internal/autostart"
	"rongsoft/miniwork/app/internal/config"
	"rongsoft/miniwork/app/internal/publish"
	"rongsoft/miniwork/app/internal/source"
	"rongsoft/miniwork/app/internal/store"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	dbPath := defaultDBPath()
	// 日志要在最前面接上：后面每一步（开库、读配置、建窗口）都可能失败，
	// 而失败原因恰恰是最需要留下来的那几行。
	closeLog := setupLog(filepath.Dir(dbPath))
	defer closeLog()

	st, err := store.Open(dbPath)
	if err != nil {
		log.Fatalf("打开本地库失败: %v", err)
	}
	defer st.Close()

	// 数据源按配置选：配了服务端地址走 remote，否则走 mock。
	// 配置读不出来不致命——退回默认配置，用户顶多是看到示例数据。
	cfg, cfgErr := config.Load()
	if cfgErr != nil {
		log.Printf("配置有异常，已按默认值启动: %v", cfgErr)
	}
	src, label := source.Build(source.Spec{
		Kind:    cfg.Source,
		BaseURL: cfg.Remote.BaseURL,
		Token:   cfg.Remote.Token,
	})
	svc := NewTopicService(st, src, label, nil, dbPath)
	// 多模型：配置里列了几个就建几个，按场景分工；没列就退回单个。
	// 客户端由 applyAIConfig 装配 —— 启动和「设置页保存 Key」走同一条路，
	// 免得出现「重启后能用、保存后不能用」这种只在一边发生的差异。
	svc.applyAIConfig(cfg.AI)
	aiClient := svc.client()
	aiName := "关闭"
	if aiClient != nil {
		// 模式和模型名都写：只看模式（byok/relay）不知道到底用哪个模型，
		// 而排查「稿子怎么变差了」第一句就是问这个。界面上显示的是具体模型名，
		// 日志要和它对得上。
		aiName = aiClient.Name() + " / " + ai.ModelNameOf(aiClient)
	}
	log.Printf("数据源：%s（%s）  模型：%s  配置：%s", src.Name(), label, aiName, config.Path())
	// 浏览器这一行必须打：用户报「扫码登录点了没反应」时，
	// 第一件事就是确认内置那份在不在、是不是被解压工具漏掉了。
	if p, ver := publish.BundledBrowser(); p != "" {
		log.Printf("内置浏览器：%s（%s）", ver, p)
	} else {
		log.Printf("内置浏览器：没找到。扫码登录会退回到系统里的 Chrome/Edge，装不了就只能手动发")
	}

	// 开机自启拉起来时不显示窗口：用户开机时不该被一个后台工具挡住视线，
	// 但定时器要照常跑。平时手动双击不带这个参数，窗口照常出现。
	startHidden := hasArg(os.Args[1:], autostart.AutostartArg)

	// sh 要在 Options 之前声明：单实例回调里要用它把已有窗口叫出来，
	// 而回调是 application.New 时就注册进去的。
	var sh *shell

	app := application.New(application.Options{
		Name:        appName,
		Description: "每天长出来的选题清单",
		// 同一时间只允许一个实例。
		//
		// Windows 上双击两次就是两个进程：两个进程打开同一个 sqlite 库、
		// 往托盘里各放一个图标、各跑一套定时器。用户看到的是「任务栏里多了个图标，
		// 但窗口没出来」——其实已经开着了。第二份自己退出，把第一份叫到前面。
		// macOS 的 .app 双击本来由系统去重，但从终端起两次同样会撞，一并管住。
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: "com.rongsoft.miniwork",
			OnSecondInstanceLaunch: func(application.SecondInstanceData) {
				if sh != nil {
					sh.showWindow()
				}
			},
		},
		Services: []application.Service{
			application.NewService(svc),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			// 最后一个窗口真的关掉时就退出应用（AppKit 的常规行为；
			// Go 这边零值是 false，也就是「关完还赖着」，所以要显式写 true）。
			//
			// 让进程在关窗后活着，靠的是 shell 在关窗事件上 Cancel + Hide ——
			// 窗口根本没关，这条规则就不会触发。反过来设成 false 会漏掉
			// 一个要命的分支：用户把「驻留」关掉再点红叉时，窗口照常销毁，
			// 而应用因为这条设置赖着不走，于是留下一个既没有窗口、
			// 也没有菜单栏图标的进程，除了活动监视器没有任何办法结束它。
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
		Windows: application.WindowsOptions{
			// 同一条道理：驻留由 shell 拦关窗实现，不靠这个开关。
			// 设成 true 会在「驻留关着」时留下同样的僵尸进程。
			DisableQuitOnLastWindowClosed: false,
		},
	})
	svc.setApp(app)

	// 定时刷新随 app 一起起来。它自己每分钟判断一次该不该跑，
	// 所以在窗口还没打开的时候也能工作（比如用户开机就放在后台）。
	svc.startScheduler()

	win := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title: appName,
		// 1180 是「左栏导航 + 一列内容」还能留出呼吸感的宽度。
		// 之前 1040 时右栏一打开，中间那块只剩三百多像素，
		// 日历和卡片全被压成一条——看着不是密，是挤。
		Width: 1180,
		// 800 而不是 750：侧栏四组导航 + 底部状态块要占掉 700 出头，
		// 矮一点「设置」和模型角标就会被挤到可视区外。
		Height:    800,
		MinWidth:  960,
		MinHeight: 600,
		Hidden:    startHidden,
		Mac: application.MacWindow{
			InvisibleTitleBarHeight: 46,
			Backdrop:                application.MacBackdropTranslucent,
			TitleBar:                application.MacTitleBarHiddenInset,
		},
		BackgroundColour: application.NewRGB(247, 246, 243),
		URL:              "/",
	})

	sh = newShell(app, svc)
	svc.setShell(sh)
	sh.start(win, startHidden)

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

// hasArg 判断启动参数里有没有某一项。
//
// 只做精确匹配，不碰 "--autostart=true" 这种写法：参数由我们自己的自启项写入，
// 格式是固定的；支持宽松匹配只会让「用户手工加了个奇怪参数」变成难查的行为偏差。
func hasArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

// toModelSpecs 把配置里的模型清单翻成 ai 包的结构。
// 两边字段一样但不共用类型：ai 不依赖 config，测试才能不造配置文件。
func toModelSpecs(list []config.Model) []ai.ModelSpec {
	out := make([]ai.ModelSpec, 0, len(list))
	for _, m := range list {
		out = append(out, ai.ModelSpec{
			ID: m.ID, Label: m.Label, BaseURL: m.BaseURL,
			APIKey: m.APIKey, Model: m.Model, Note: m.Note,
		})
	}
	return out
}

// defaultDBPath 放在用户配置目录，卸载/升级不丢数据。
func defaultDBPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = "."
	}
	p := filepath.Join(dir, "miniwork", "miniwork.db")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		log.Printf("创建数据目录失败，退化为当前目录: %v", err)
		return "./miniwork.db"
	}
	return p
}
