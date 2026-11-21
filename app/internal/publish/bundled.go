package publish

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// 内置浏览器：把一份 Chromium 跟着应用一起发出去。
//
// 为什么必须内置：小红书和抖音只能靠浏览器驱动，「扫码登录」这件事
// 没浏览器就是死路。而客户机器上装没装 Chrome 完全不可控——
// 我们自己的开发机上就没装（只有 Playwright 缓存里那份），
// 用户看到的就是一个点了就报错的「扫码登录」按钮。
//
// 内置之后，能不能发布不再取决于用户装过什么。

// bundledDirName 是浏览器目录在包里的名字。
const bundledDirName = "browser"

// layout 是一个平台上内置浏览器的落点。
//
// 抽出来是为了能在 macOS 上验 Windows 的布局：打包在 Windows 机器上做，
// 但改代码是在这里，没有这层就没法测「装完之后程序找不找得到浏览器」。
type layout struct {
	Goos string // darwin / windows / linux
	Arch string // amd64 / arm64
	Tag  string // browser/ 下的平台目录名，与 fetch-browser.sh 一致
	Exe  string // 相对该目录的可执行文件，用 / 分隔
}

// layoutFor 按目标平台给出落点。
//
// 平台目录名用的就是 Chrome for Testing 自己的命名（mac-arm64 / win64 /
// linux64），下载和查找共用一套映射，不用在两边各维护一张表。
func layoutFor(goos, goarch string) layout {
	switch goos {
	case "darwin":
		tag := "mac-x64"
		if goarch == "arm64" {
			tag = "mac-arm64"
		}
		return layout{goos, goarch, tag,
			"Google Chrome for Testing.app/Contents/MacOS/Google Chrome for Testing"}
	case "windows":
		// Chrome for Testing 只有 win64，没有 win-arm64。
		return layout{goos, goarch, "win64", "chrome.exe"}
	default:
		return layout{goos, goarch, "linux64", "chrome"}
	}
}

func platTag() string    { return layoutFor(runtime.GOOS, runtime.GOARCH).Tag }
func exeRelPath() string { return layoutFor(runtime.GOOS, runtime.GOARCH).Exe }

// bundleRoots 列出可能放着 browser/ 的目录，按可信度排。
//
// 同一个二进制会在三种形态下运行，资源位置各不相同：
//   - 装好的 .app：可执行文件在 Contents/MacOS/ 下，资源在 Contents/Resources/；
//   - Windows / Linux 绿色包：浏览器就在 exe 旁边；
//   - 开发时 bin/Miniwork 裸跑：浏览器在仓库根的 browser/。
//
// 三种都试一遍，比启动时读环境变量猜要可靠。
//
// MW_BROWSER_ROOT 可以覆盖：把 370MB 的浏览器放在共享盘，或者
// 打包时忘了带想临时指过去，都不用重新编译。
func bundleRoots() []string {
	var roots []string
	if d := os.Getenv("MW_BROWSER_ROOT"); d != "" {
		roots = append(roots, d)
	}
	exe, err := os.Executable()
	if err != nil {
		return roots
	}
	// 用户可能从别处软链过来启动，取真实路径，否则上一层会算到错的目录。
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	dir := filepath.Dir(exe)
	return append(roots, []string{
		filepath.Join(dir, "..", "Resources"), // macOS .app 包内（发行版走这条）
		dir,                                   // Windows / Linux / 裸二进制
		filepath.Join(dir, ".."),              // 开发期：bin/ 的上一层就是仓库根
		// 开发期从 bin/Miniwork.dev.app 里跑：Contents/MacOS 往上四层才是仓库根。
		// 有了这条，开发时不用把 370MB 的浏览器拷进 app 包，直接读仓库里那份。
		// 排在最后：发行版压根没有 .dev 结构，不会误命中。
		filepath.Join(dir, "..", "..", "..", ".."),
	}...)
}

// usable 判断这个可执行文件能不能真的跑起来（按当前平台）。
func usable(p string) bool { return usableFor(runtime.GOOS, p) }

// usableFor 按指定平台判断可执行文件是否完整。
//
// 不能只看 os.Stat：这个 exe 自己只是个小壳，真正的实现在旁边。
//   - macOS：.app 里那个同名文件几十 KB，实现在 Contents/Frameworks；
//   - Windows：chrome.exe 旁边必须有 chrome.dll，否则进程起来立刻退出。
//
// 拷贝时少带了那一半，文件照样存在、版本号也读得到，一启动就崩，
// 而报错只有一句 "exit status 1"——所以这里连旁边那份一起核实。
func usableFor(goos, p string) bool {
	fi, err := os.Stat(p)
	if err != nil || fi.IsDir() {
		return false
	}
	switch goos {
	case "darwin":
		fr := filepath.Join(filepath.Dir(p), "..", "Frameworks")
		if d, err := os.Stat(fr); err != nil || !d.IsDir() {
			return false
		}
	case "windows":
		dll, err := os.Stat(filepath.Join(filepath.Dir(p), "chrome.dll"))
		if err != nil || dll.IsDir() {
			return false
		}
	}
	return true
}

// BundledBrowser 返回内置浏览器的路径和版本号。
//
// 找不到就返回空串，不报错：调用方需要区分「没有内置」和「内置坏了」，
// 这两种在界面上该说的话不一样。
func BundledBrowser() (path, version string) {
	return bundledFor(bundleRoots(), layoutFor(runtime.GOOS, runtime.GOARCH))
}

// bundledIn 在给定的几个根目录里找当前平台那份。
func bundledIn(roots []string) (path, version string) {
	return bundledFor(roots, layoutFor(runtime.GOOS, runtime.GOARCH))
}

// bundledFor 按指定布局找。拆出来只为可测：
// 真机上路径是算出来的，测试里没法伪造 os.Executable 和目标平台。
func bundledFor(roots []string, l layout) (path, version string) {
	for _, root := range roots {
		platDir := filepath.Join(root, bundledDirName, l.Tag)
		p := filepath.Join(platDir, filepath.FromSlash(l.Exe))
		if !usableFor(l.Goos, p) {
			continue
		}
		raw, _ := os.ReadFile(filepath.Join(platDir, "VERSION"))
		return p, strings.TrimSpace(string(raw))
	}
	return "", ""
}

// IsBundled 判断这个路径是不是包里自带的那一份。
//
// 界面要说「你不用装 Chrome，用的是自带的」还是「用的是你系统中的 Chrome」，
// 这两句话不一样：前者可以承诺行为稳定，后者得提醒版本可能影响选择器。
func IsBundled(path string) bool {
	b, _ := BundledBrowser()
	return b != "" && path == b
}
