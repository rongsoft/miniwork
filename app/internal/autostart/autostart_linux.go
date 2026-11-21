//go:build linux

package autostart

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// 开机自启时带这个参数：启动后不弹窗口，直接进托盘。
const AutostartArg = "--autostart"

func supported() bool { return true }

func location(label string) string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "autostart", label+".desktop")
}

// execLine 是 .desktop 里的 Exec 值。
//
// 按 freedesktop 规范，含空格的路径必须加双引号，且值里的反斜杠要转义。
// Exec 不是 shell，所以不能用 $HOME 这类变量展开。
func (m *Manager) execLine() string {
	return `"` + strings.ReplaceAll(m.exe, `\`, `\\`) + `" ` + AutostartArg
}

func (m *Manager) installed() bool {
	raw, err := os.ReadFile(m.target())
	if err != nil {
		return false
	}
	body := string(raw)
	if !strings.Contains(body, m.execLine()) {
		return false
	}
	// 有的桌面环境靠这一行决定要不要跑，显式关掉的话不算启用。
	return !strings.Contains(body, "X-GNOME-Autostart-enabled=false")
}

func (m *Manager) install() error {
	path := m.target()
	if path == "" {
		return fmt.Errorf("找不到用户配置目录，无法设置开机自启")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("创建 autostart 目录失败: %w", err)
	}

	var sb strings.Builder
	sb.WriteString("[Desktop Entry]\n")
	sb.WriteString("Type=Application\n")
	sb.WriteString("Name=Miniwork\n")
	sb.WriteString("Comment=选题助手\n")
	sb.WriteString("Exec=" + m.execLine() + "\n")
	sb.WriteString("Terminal=false\n")
	sb.WriteString("X-GNOME-Autostart-enabled=true\n")

	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		return fmt.Errorf("写入自启配置失败: %w", err)
	}
	return nil
}

func (m *Manager) remove() error {
	path := m.target()
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("删除自启配置失败: %w", err)
	}
	return nil
}
