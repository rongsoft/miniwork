// Package autostart 负责「开机自动运行本程序」。
//
// 为什么要这个包：定时刷新是靠进程内的 ticker 推的，程序不运行就不刷。
// 用户在设置里打开了「每天自动刷新」，第二天开机却发现什么都没发生——
// 不是定时器算错了，是它压根没跑起来。自启就是把这条链补完整。
//
// 三个平台的机制完全不同，但语义只有一个：登录后自动启动本程序。
// 各平台的实现放在 autostart_<goos>.go，这里只放共用类型与路径判定。
package autostart

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Label 是自启项在系统里的唯一标识。用反写域名，和 macOS 的惯例一致。
const Label = "com.rongsoft.miniwork"

// Status 是给界面看的自启状态。
type Status struct {
	Supported bool   `json:"supported"` // 当前平台有没有实现
	Enabled   bool   `json:"enabled"`
	Location  string `json:"location"` // 自启项在磁盘上的位置，给用户看
	Reason    string `json:"reason"`   // 不能开启时的原因（开发态等）；能开时为空
}

// Manager 封装自启项的增删查。exe 与 path 可注入，便于测试。
type Manager struct {
	exe   string
	label string
	path  string // 自启项文件路径；为空时按平台惯例推算
}

// New 用当前可执行文件路径建一个 Manager。
func New() *Manager {
	exe, err := os.Executable()
	if err != nil {
		exe = ""
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return &Manager{exe: exe, label: Label}
}

// NewWith 用于测试：指定任意的可执行文件路径。
func NewWith(exe string) *Manager { return &Manager{exe: exe, label: Label} }

// newAt 用于测试：把自启项落到指定的临时路径，
// 避免单测往用户真实的 LaunchAgents / 注册表里写东西。
func newAt(exe, path string) *Manager {
	return &Manager{exe: exe, label: Label, path: path}
}

// target 返回自启项应该写到哪里。
func (m *Manager) target() string {
	if m.path != "" {
		return m.path
	}
	return location(m.label)
}

// Check 返回当前状态。它不是纯查询——会顺带算出「能不能开」。
func (m *Manager) Check() Status {
	st := Status{Location: m.target()}

	if err := m.blockReason(); err != "" {
		st.Reason = err
		return st
	}
	st.Supported = supported()
	if !st.Supported {
		st.Reason = "当前系统暂不支持开机自启"
		return st
	}
	st.Enabled = m.installed()
	return st
}

// Enable 装上自启项。已存在时覆盖（用户可能换了安装位置）。
func (m *Manager) Enable() error {
	if reason := m.blockReason(); reason != "" {
		return fmt.Errorf("%s", reason)
	}
	if !supported() {
		return fmt.Errorf("当前系统暂不支持开机自启")
	}
	return m.install()
}

// Disable 删掉自启项。不存在也算成功——用户的目的达到了。
func (m *Manager) Disable() error {
	if !supported() {
		return fmt.Errorf("当前系统暂不支持开机自启")
	}
	return m.remove()
}

// blockReason 回答「为什么现在不能写自启」。
//
// 最要紧的一条是开发态：`go run` 会在临时目录里造一个二进制，
// 把这个路径写进自启项，下次开机会去执行一个早就被删掉的文件——
// 而且用户完全看不出为什么失效。宁可不装，也不能装个假的。
func (m *Manager) blockReason() string {
	if strings.TrimSpace(m.exe) == "" {
		return "无法确定程序位置，不能设置开机自启"
	}
	// go run / go test 的产物都在 go-build 目录下，进程退出即删。
	if strings.Contains(m.exe, "go-build") {
		return "这是开发中运行的临时程序，重启后不存在，不能设置开机自启。请用打包后的应用。"
	}
	tmp := filepath.Clean(os.TempDir())
	if strings.HasPrefix(filepath.Clean(m.exe), tmp+string(filepath.Separator)) {
		return "程序在临时目录里运行，重启后不存在，不能设置开机自启。"
	}
	return ""
}

// launchTarget 返回自启项该执行什么。
//
// macOS 上有个坑：直接执行 .app/Contents/MacOS/ 里的二进制，进程能起来，
// 但拿不到完整的 bundle 环境（Dock 图标、激活策略都会异常）。
// 所以打包形态必须用 `open -a <app路径>` 让系统来启动它。
func (m *Manager) launchTarget() (cmd string, args []string) {
	if runtime.GOOS == "darwin" {
		if app := appBundle(m.exe); app != "" {
			return "/usr/bin/open", []string{"-a", app}
		}
	}
	return m.exe, nil
}

// appBundle 从 .../Foo.app/Contents/MacOS/Foo 反推出 .../Foo.app。
// 不在 .app 里就返回空串。
func appBundle(exe string) string {
	// 分隔符写死 "/" 而不是 filepath.Separator：.app 的内部结构是 macOS
	// 定的，无论在哪个平台上看，它都是 /Contents/MacOS/。
	// 用平台分隔符会在 Windows 上把 marker 拼成 ".app\Contents\"，
	// 于是一个合法的 bundle 路径被判断成「不在 .app 里」，永远返回空串——
	// 这条路径本身只在 darwin 上走得到，但判断逻辑不该依赖宿主平台。
	marker := ".app/Contents/"
	i := strings.Index(exe, marker)
	if i < 0 {
		return ""
	}
	return exe[:i+len(".app")]
}

// platform 给各平台实现留一个统一的编译期开关，避免在共享文件里写 if runtime。
func platformName() string { return runtime.GOOS }
