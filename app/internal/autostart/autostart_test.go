package autostart

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// 开发态必须被拒。
//
// go run 产生的二进制在临时目录里，进程退出就被删。
// 把那个路径写进自启项，用户下次开机会去执行一个不存在的文件，
// 而且不会有任何提示——这种「静默失效」比直接报错难查得多。
func TestRejectDevBuild(t *testing.T) {
	cases := []string{
		"/var/folders/xy/T/go-build1234/b001/exe/app",
		"/home/dev/.cache/go-build/ab/cd/exe/app",
	}
	for _, exe := range cases {
		m := NewWith(exe)
		if reason := m.blockReason(); reason == "" {
			t.Fatalf("%s 是开发态路径，应该被拒绝", exe)
		}
		if err := m.Enable(); err == nil {
			t.Fatalf("%s 不该允许开启自启", exe)
		}
		st := m.Check()
		if st.Supported || st.Enabled {
			t.Fatalf("%s 被拒时不该报告 supported/enabled：%+v", exe, st)
		}
		if st.Reason == "" {
			t.Fatalf("%s 被拒时必须给出原因，否则界面只能干瞪眼", exe)
		}
	}
}

// 临时目录下的程序同样拒绝。
func TestRejectTempDir(t *testing.T) {
	exe := filepath.Join(os.TempDir(), "miniwork-copy")
	if reason := NewWith(exe).blockReason(); reason == "" {
		t.Fatalf("%s 在临时目录里，应该被拒绝", exe)
	}
}

// 正常安装路径必须放行，否则这个功能永远用不了。
func TestAcceptInstalledPath(t *testing.T) {
	exe := "/Applications/Miniwork.app/Contents/MacOS/Miniwork"
	if reason := NewWith(exe).blockReason(); reason != "" {
		t.Fatalf("正常安装路径不该被拒，得到：%s", reason)
	}
}

// .app 反推：打包形态必须用 `open -a` 启动，直接执行内层二进制拿不到 bundle 环境。
func TestAppBundle(t *testing.T) {
	got := appBundle("/Applications/Miniwork.app/Contents/MacOS/Miniwork")
	want := "/Applications/Miniwork.app"
	if got != want {
		t.Fatalf("应反推出 %q，得到 %q", want, got)
	}
	if got := appBundle("/usr/local/bin/miniwork"); got != "" {
		t.Fatalf("不在 .app 里应返回空串，得到 %q", got)
	}
}

// 装了、查得到、能删——完整一轮。测试落在临时目录，不碰用户真实配置。
func TestInstallCheckRemove(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("文件型自启项只在这两个平台验证")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "com.rongsoft.miniwork.test.plist")
	exe := "/Applications/Miniwork.app/Contents/MacOS/Miniwork"

	m := newAt(exe, path)
	if m.installed() {
		t.Fatal("还没装就报已启用")
	}
	if err := m.Enable(); err != nil {
		t.Fatalf("开启失败: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("自启项没写出来: %v", err)
	}
	body := string(raw)
	if runtime.GOOS == "darwin" {
		// 打包形态必须是 open -a，而不是直接指向内层二进制。
		if !strings.Contains(body, "/usr/bin/open") {
			t.Fatalf(".app 形态应该用 open 启动，实际内容：\n%s", body)
		}
		if !strings.Contains(body, AutostartArg) {
			t.Fatalf("启动参数丢了，开机时会弹窗口：\n%s", body)
		}
		if !strings.Contains(body, "<key>RunAtLoad</key>") {
			t.Fatalf("缺 RunAtLoad，开机不会跑：\n%s", body)
		}
		// 刻意不设 KeepAlive：用户点「退出」后不该被系统拉回来。
		if strings.Contains(body, "KeepAlive") {
			t.Fatalf("不该设 KeepAlive，否则「退出」菜单就是骗人的：\n%s", body)
		}
	}

	if !m.installed() {
		t.Fatal("装完却查不到，开关会显示成关的")
	}

	if err := m.Disable(); err != nil {
		t.Fatalf("关闭失败: %v", err)
	}
	if m.installed() {
		t.Fatal("删完还报已启用")
	}
	// 重复关闭不能报错：用户的目的（没有自启项）已经达到了。
	if err := m.Disable(); err != nil {
		t.Fatalf("重复关闭应幂等，得到: %v", err)
	}
}

// 最关键的一条：程序换了位置，旧自启项必须被判为「未启用」。
//
// 只看文件在不在的话，用户把 app 从下载目录拖到「应用程序」后，
// 开关显示「已开启」，下次开机却什么都不发生。
func TestStaleEntryIsNotEnabled(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("文件型自启项只在这两个平台验证")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "stale.plist")

	oldPath := "/Users/someone/Downloads/Miniwork.app/Contents/MacOS/Miniwork"
	if err := newAt(oldPath, path).Enable(); err != nil {
		t.Fatal(err)
	}

	// 用户把程序移到了「应用程序」文件夹。
	moved := newAt("/Applications/Miniwork.app/Contents/MacOS/Miniwork", path)
	if moved.installed() {
		t.Fatal("自启项还指着旧路径，不能算已启用——否则开关是假的")
	}
	// 重新开启应该覆盖成新路径，而不是不报错地什么都不做。
	if err := moved.Enable(); err != nil {
		t.Fatal(err)
	}
	if !moved.installed() {
		t.Fatal("覆盖后应指向新路径")
	}
}
