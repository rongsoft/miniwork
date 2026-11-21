//go:build !darwin && !windows && !linux

package autostart

import "os"

// 其他平台（FreeBSD 等）暂不实现。返回不支持而不是假装支持，
// 界面会根据 Supported 把开关置灰。
const AutostartArg = "--autostart"

func supported() bool { return false }

func location(string) string { return "" }

func (m *Manager) installed() bool { return false }

func (m *Manager) install() error { return os.ErrInvalid }

func (m *Manager) remove() error { return os.ErrInvalid }
