package main

import (
	"fmt"
	"log"

	"rongsoft/miniwork/app/internal/autostart"
)

// 设置页「后台运行」那一格。
//
// 为什么这个功能值得单独一块：定时刷新是这个产品的核心承诺（打开就有新东西），
// 但它靠进程里的 ticker 推。用户关机、关窗口、退出应用，定时器就停——
// 而界面上写着「每天 08:30 自动刷新」，他不会想到「前提是这东西得一直开着」。
// 自启和驻留就是把这句话的前提补上，不是两个可有可无的开关。

// ShellView 是给界面的后台运行状态。
type ShellView struct {
	Resident bool `json:"resident"` // 关窗后驻留（macOS 菜单栏 / Windows 托盘）

	// AutoStart 直接用系统里的真实状态，不存配置。
	//
	// 存配置就会出现「配置说开着、系统里其实没有」——用户重装了程序、
	// 或者把 app 从下载目录拖到应用程序文件夹，配置还写着已开启，
	// 开关是绿的，开机什么都不会发生。以系统为准就没有这种假象。
	AutoStart autostart.Status `json:"autoStart"`
}

// AppShell 返回后台运行的当前状态。
func (s *TopicService) AppShell() ShellView {
	v := ShellView{AutoStart: autostart.New().Check()}
	if s.sh != nil {
		v.Resident = s.sh.isResident()
	}
	return v
}

// SetResident 切换「关窗后驻留」。
func (s *TopicService) SetResident(on bool) error {
	if s.sh == nil {
		return fmt.Errorf("桌面外壳还没准备好，请稍后再试")
	}
	s.sh.setResident(on)
	return nil
}

// SetAutoStart 开关开机自启，返回操作之后系统里的真实状态。
//
// 返回状态而不是直接返回 error，是因为「失败」有好几种：不支持、开发态、
// 写文件出错。前两种是用户需要看到原因才知道该怎么办的情况，
// 只回一句 error 界面只能弹个红字，帮不上忙。
func (s *TopicService) SetAutoStart(on bool) (autostart.Status, error) {
	m := autostart.New()

	if on {
		// 开自启的同时把驻留打开。
		//
		// 不这么做的话「自启 + 关窗即退」是一种自相矛盾的配置：开机启动、
		// 用户碰一下窗口就退出，等于开机时闪了一下，定时器一分钟都没跑满。
		// 两件事本来就该一起开，界面上的联动只是把这句话说清楚。
		if s.sh != nil && !s.sh.isResident() {
			s.sh.setResident(true)
		}
		if err := m.Enable(); err != nil {
			return m.Check(), err
		}
		log.Printf("已开启开机自启")
		return m.Check(), nil
	}

	if err := m.Disable(); err != nil {
		return m.Check(), err
	}
	log.Printf("已关闭开机自启")
	return m.Check(), nil
}
