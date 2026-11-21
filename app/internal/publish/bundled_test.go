package publish

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// fakeBundle 在 dir 下造一份「看起来像真的」内置浏览器。
// macOS 上连 Frameworks 一起造，因为 usable() 会核实它。
func fakeBundle(t *testing.T, root string) string {
	t.Helper()
	dir := filepath.Join(root, bundledDirName, platTag())
	exe := filepath.Join(dir, filepath.FromSlash(exeRelPath()))
	if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "darwin" {
		if err := os.MkdirAll(filepath.Join(filepath.Dir(exe), "..", "Frameworks"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// Windows 上 usable() 会连带核实 chrome.dll 在不在（只有 exe 的话
	// 进程起来立刻退出）。夹具少造这一个文件，「能找内置浏览器」这条
	// 就会在 Windows 上永远判失败——看着像功能坏了，其实只是造得不像。
	if runtime.GOOS == "windows" {
		if err := os.WriteFile(filepath.Join(dir, "chrome.dll"), []byte("MZ"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "VERSION"), []byte("157.0.8103.78\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return exe
}

func TestBundledFoundWithVersion(t *testing.T) {
	root := t.TempDir()
	want := fakeBundle(t, root)

	got, ver := bundledIn([]string{root})
	if got != want {
		t.Fatalf("路径 = %q，应该是 %q", got, want)
	}
	// 版本号末尾有换行，必须去掉——它会直接显示在界面上，
	// 带个换行就会出现半行空白。
	if ver != "157.0.8103.78" {
		t.Fatalf("版本 = %q", ver)
	}
}

// 没有内置是正常状态（比如从源码跑测试），不是错误。
func TestBundledAbsentIsNotAnError(t *testing.T) {
	got, ver := bundledIn([]string{t.TempDir(), t.TempDir()})
	if got != "" || ver != "" {
		t.Fatalf("不该找到东西，却拿到 %q / %q", got, ver)
	}
}

// macOS 上那个可执行文件只是几十 KB 的壳，真正实现在 Frameworks 里。
// 拷贝不全时文件照样存在，但一启动就崩——不能只看文件在不在。
func TestBundledRejectsHalfCopiedBundle(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("这条只对 macOS 的 .app 结构有意义")
	}
	root := t.TempDir()
	exe := fakeBundle(t, root)
	// 把 Frameworks 拿掉，模拟「只拷了个壳」。
	if err := os.RemoveAll(filepath.Join(filepath.Dir(exe), "..", "Frameworks")); err != nil {
		t.Fatal(err)
	}

	if got, _ := bundledIn([]string{root}); got != "" {
		t.Fatalf("半份的包不该被当成可用，却拿到 %q", got)
	}
}

// 一个根目录里是半份，下一个是好的：要跳过坏的继续找。
// 真实场景是开发机上仓库里那份被中断的拷贝盖住了包里的好副本。
func TestBundledSkipsBrokenRoot(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("这条依赖 Frameworks 校验")
	}
	bad := t.TempDir()
	exe := fakeBundle(t, bad)
	if err := os.RemoveAll(filepath.Join(filepath.Dir(exe), "..", "Frameworks")); err != nil {
		t.Fatal(err)
	}
	good := t.TempDir()
	want := fakeBundle(t, good)

	got, _ := bundledIn([]string{bad, good})
	if got != want {
		t.Fatalf("该跳过坏的用好的，拿到 %q", got)
	}
}

// 排在前面的根目录优先：包内的那份要压过仓库里开发用的那份。
func TestBundledFirstRootWins(t *testing.T) {
	first := t.TempDir()
	want := fakeBundle(t, first)
	second := t.TempDir()
	fakeBundle(t, second)

	got, _ := bundledIn([]string{first, second})
	if got != want {
		t.Fatalf("拿到 %q，应该用第一个根目录里那份", got)
	}
}

// fakeWinBundle 在 root 下造一份 Windows 安装后的布局：
// browser/win64/{chrome.exe, chrome.dll, VERSION}。
// withDLL=false 用来模拟「只拷了 exe 的半份拷贝」。
func fakeWinBundle(t *testing.T, root string, withDLL bool) string {
	t.Helper()
	dir := filepath.Join(root, bundledDirName, "win64")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, "chrome.exe")
	if err := os.WriteFile(exe, []byte("MZ"), 0o755); err != nil {
		t.Fatal(err)
	}
	if withDLL {
		if err := os.WriteFile(filepath.Join(dir, "chrome.dll"), []byte("MZ"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "VERSION"), []byte("157.0.8103.78\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return exe
}

// Windows 安装包把浏览器放在 $INSTDIR\browser\win64\ 下，
// 程序从 exe 同级目录去找——这条路径必须在 Mac 上就能验。
func TestBundledWindowsLayout(t *testing.T) {
	root := t.TempDir()
	want := fakeWinBundle(t, root, true)

	got, ver := bundledFor([]string{root}, layoutFor("windows", "amd64"))
	if got != want {
		t.Fatalf("路径 = %q，应该是 %q", got, want)
	}
	if ver != "157.0.8103.78" {
		t.Fatalf("版本 = %q", ver)
	}
}

// chrome.exe 旁边没有 chrome.dll 时进程起来即退，报错只有 exit status 1。
// 这种半份包必须判死，否则用户看到的是「浏览器在、点了没反应」。
func TestBundledWindowsRejectsMissingDLL(t *testing.T) {
	root := t.TempDir()
	fakeWinBundle(t, root, false)

	if got, _ := bundledFor([]string{root}, layoutFor("windows", "amd64")); got != "" {
		t.Fatalf("缺 chrome.dll 的包不该被当成可用，却拿到 %q", got)
	}
}

// Chrome for Testing 没有 win-arm64：ARM 版 Windows 也只能找 win64 目录，
// 找不到就回落到系统 Chrome，而不是去一个永远不存在的目录里找。
func TestBundledWindowsArmUsesSameTag(t *testing.T) {
	l := layoutFor("windows", "arm64")
	if l.Tag != "win64" || l.Exe != "chrome.exe" {
		t.Fatalf("win-arm64 应该沿用 win64 的落点，实际是 %+v", l)
	}
}

// 三个平台的目录名必须和 scripts/fetch-browser.sh 的 DEST 一致，
// 否则「下载好了但程序找不到」——两边各维护一张表迟早会错。
func TestBundledTagsMatchFetchScript(t *testing.T) {
	cases := []struct {
		goos, goarch, tag string
	}{
		{"darwin", "arm64", "mac-arm64"},
		{"darwin", "amd64", "mac-x64"},
		{"windows", "amd64", "win64"},
		{"linux", "amd64", "linux64"},
	}
	for _, c := range cases {
		if got := layoutFor(c.goos, c.goarch).Tag; got != c.tag {
			t.Errorf("%s/%s 的目录名 = %q，应该是 %q", c.goos, c.goarch, got, c.tag)
		}
	}
}

// 安装路径必须和 scripts/fetch-browser.sh 摆的位置一致。
// 这条写死字符串是有意的：脚本改名时测试要跟着红，而不是运行时静默找不到。
func TestBundledLayoutMatchesFetchScript(t *testing.T) {
	root := "/R"
	got := filepath.Join(root, bundledDirName, platTag(), filepath.FromSlash(exeRelPath()))
	switch runtime.GOOS {
	case "darwin":
		want := "/R/browser/" + platTag() + "/Google Chrome for Testing.app/Contents/MacOS/Google Chrome for Testing"
		if got != want {
			t.Fatalf("路径 = %q\n应该 = %q", got, want)
		}
	case "windows":
		if filepath.Base(got) != "chrome.exe" {
			t.Fatalf("Windows 上应该找 chrome.exe，却是 %q", got)
		}
	default:
		if filepath.Base(got) != "chrome" {
			t.Fatalf("Linux 上应该找 chrome，却是 %q", got)
		}
	}
}
