//go:build darwin

package autostart

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// 开机自启时带这个参数：程序启动后不弹窗口，直接进托盘。
// 用户开机时不该被一个后台工具挡住视线，但定时器要照常跑。
const AutostartArg = "--autostart"

func supported() bool { return true }

func location(label string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "Library", "LaunchAgents", label+".plist")
}

// installed 判断自启项是否存在**且指向当前程序**。
//
// 只看文件在不在是不够的：用户把 app 从下载目录拖到「应用程序」文件夹后，
// 旧 plist 还指着原来的路径，开关会显示「已开启」但下次开机什么都不会发生。
// 这种假成功比明确的失败更难查。
func (m *Manager) installed() bool {
	raw, err := os.ReadFile(m.target())
	if err != nil {
		return false
	}
	body := string(raw)
	cmd, args := m.launchTarget()
	if !strings.Contains(body, xmlEscape(cmd)) {
		return false
	}
	for _, a := range args {
		if !strings.Contains(body, xmlEscape(a)) {
			return false
		}
	}
	// .app 形态下写的是外层 .app 路径（交给 open 去启动），内层二进制路径
	// 根本不出现在文件里，所以这里只能比到启动目标这一层。
	// 直接跑二进制时才需要确认路径本身。
	if cmd == m.exe && !strings.Contains(body, xmlEscape(m.exe)) {
		return false
	}
	return strings.Contains(body, xmlEscape(m.label))
}

func (m *Manager) install() error {
	path := m.target()
	if path == "" {
		return fmt.Errorf("找不到用户目录，无法设置开机自启")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("创建 LaunchAgents 目录失败: %w", err)
	}

	cmd, args := m.launchTarget()
	progArgs := append([]string{cmd}, args...)
	progArgs = append(progArgs, "--args", AutostartArg)
	// 直接跑二进制时没有 --args 这一层，参数要直接跟在后面。
	if cmd == m.exe {
		progArgs = []string{m.exe, AutostartArg}
	}

	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	sb.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	sb.WriteString("<plist version=\"1.0\">\n<dict>\n")
	sb.WriteString("  <key>Label</key>\n  <string>" + xmlEscape(m.label) + "</string>\n")
	sb.WriteString("  <key>ProgramArguments</key>\n  <array>\n")
	for _, a := range progArgs {
		sb.WriteString("    <string>" + xmlEscape(a) + "</string>\n")
	}
	sb.WriteString("  </array>\n")
	sb.WriteString("  <key>RunAtLoad</key>\n  <true/>\n")
	// 不设 KeepAlive：用户主动退出后不该被系统反复拉起，
	// 那样「退出」这个菜单项就是骗人的。
	sb.WriteString("</dict>\n</plist>\n")

	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		return fmt.Errorf("写入自启配置失败: %w", err)
	}

	// 已加载过的话，launchd 手里还是旧的那份，必须重新加载。
	// 这两步失败不算致命：文件已经写好了，下次登录一定会读到新内容。
	_ = exec.Command("launchctl", "unload", path).Run()
	_ = exec.Command("launchctl", "load", path).Run()
	return nil
}

func (m *Manager) remove() error {
	path := m.target()
	if path == "" {
		return nil
	}
	_ = exec.Command("launchctl", "unload", path).Run()
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("删除自启配置失败: %w", err)
	}
	return nil
}

// xmlEscape 处理 plist 里必须转义的那几个字符。
// 路径里出现 & 或 < 是小概率事件，但一旦出现就是加载失败，且报错看不懂。
func xmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")
	return r.Replace(s)
}
