//go:build windows

package autostart

import (
	"fmt"

	"golang.org/x/sys/windows/registry"
)

// 开机自启时带这个参数：启动后不弹窗口，直接进托盘。
const AutostartArg = "--autostart"

// 写 HKCU 而不是 HKLM：自启是「这台机器上的这个用户」的事，不需要管理员权限。
// 写 HKLM 会弹 UAC，为了一个开关弹权限框不值得。
const runKeyPath = `Software\Microsoft\Windows\CurrentVersion\Run`

func supported() bool { return true }

func location(label string) string {
	return `HKEY_CURRENT_USER\` + runKeyPath + `\` + label
}

// command 是注册表里存的那条命令行。
// 路径必须带引号：Program Files 里的空格会让 Windows 把路径截断。
func (m *Manager) command() string {
	return `"` + m.exe + `" ` + AutostartArg
}

func (m *Manager) installed() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()

	v, _, err := k.GetStringValue(m.label)
	if err != nil {
		return false
	}
	// 指向旧位置的条目算「未启用」：用户移动了程序，旧条目开机只会报文件找不到。
	return v == m.command()
}

func (m *Manager) install() error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("打开启动项注册表失败: %w", err)
	}
	defer k.Close()

	if err := k.SetStringValue(m.label, m.command()); err != nil {
		return fmt.Errorf("写入启动项失败: %w", err)
	}
	return nil
}

func (m *Manager) remove() error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("打开启动项注册表失败: %w", err)
	}
	defer k.Close()

	if err := k.DeleteValue(m.label); err != nil && err != registry.ErrNotExist {
		return fmt.Errorf("删除启动项失败: %w", err)
	}
	return nil
}
