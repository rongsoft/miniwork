module rongsoft/miniwork/app

go 1.27.3

require (
	github.com/chromedp/cdproto v0.0.0-20250403032234-65de8f5d025b
	github.com/chromedp/chromedp v0.13.6
	github.com/wailsapp/wails/v3 v3.0.0
	golang.org/x/image v0.45.0
	golang.org/x/sys v0.49.0
	modernc.org/sqlite v1.62.0
)

require (
	github.com/adrg/xdg v0.5.3 // indirect
	github.com/chromedp/sysutil v1.1.0 // indirect
	github.com/coder/websocket v1.8.14 // indirect
	github.com/dustin/go-humanize v1.1.0 // indirect
	github.com/go-json-experiment/json v0.0.0-20260213210345-44df1a37e875 // indirect
	github.com/go-ole/go-ole v1.3.0 // indirect
	github.com/gobwas/httphead v0.1.0 // indirect
	github.com/gobwas/pool v0.2.1 // indirect
	github.com/gobwas/ws v1.4.0 // indirect
	github.com/godbus/dbus/v5 v5.2.2 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/ncruces/go-strftime v1.1.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	golang.org/x/text v0.42.0 // indirect
	modernc.org/libc v1.80.1 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.12.1 // indirect
)

// 钉住 json-experiment 的版本。
//
// chromedp 的 cdproto 用的就是这个库，而它新版在 Go 1.27（默认开 jsonv2
// 实验）下会编译失败：alias.go 里引用的 Encoding/json/v2 符号跟标准库对不上，
// 报 json.SkipFunc undefined。钉回 cdproto 自己声明的那个版本即可，
// wails 用到的那部分 API 老版本也有。
//
// 升级 chromedp 时先删掉这一行试一遍，能编过就可以删。
replace github.com/go-json-experiment/json => github.com/go-json-experiment/json v0.0.0-20250211171154-1ae217ad3535
