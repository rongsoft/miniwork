package main

import (
	"path/filepath"
	"testing"

	"rongsoft/miniwork/app/internal/store"
)

// 启动参数只做精确匹配。
//
// 宽松匹配（前缀、包含）会让 "--autostart=false" 这种写法也被当成「要静默启动」，
// 用户手工加参数调试时会得到与预期相反的行为，而且很难看出为什么。
func TestHasArg(t *testing.T) {
	cases := []struct {
		args []string
		want string
		ok   bool
	}{
		{nil, "--autostart", false},
		{[]string{"--autostart"}, "--autostart", true},
		{[]string{"--verbose", "--autostart"}, "--autostart", true},
		{[]string{"--autostart=false"}, "--autostart", false},
		{[]string{"--autostartx"}, "--autostart", false},
		{[]string{"--auto"}, "--autostart", false},
	}
	for _, c := range cases {
		if got := hasArg(c.args, c.want); got != c.ok {
			t.Fatalf("hasArg(%v, %q) = %v，应为 %v", c.args, c.want, got, c.ok)
		}
	}
}

// 关窗驻留的默认值必须是「开」，而且要靠一个不存在的键表达。
//
// 这条测试盯的是兼容性：老库升上来没有 tray_disabled 这个键，
// 如果哪天有人把判断写成 `GetSetting(...) == "1"`，所有老用户
// 会在升级后突然变成「关窗即退出」，而他们什么都没改。
func TestTrayResidentDefaultsOn(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if v := st.GetSetting(store.SettingTrayDisabled); v != "" {
		t.Fatalf("新库里不该有 %s，实际 %q", store.SettingTrayDisabled, v)
	}
	// 与 shell.start 里的判断保持一致：不是 "1" 就算驻留。
	if st.GetSetting(store.SettingTrayDisabled) == "1" {
		t.Fatal("缺省应视为驻留开启")
	}

	// 关掉之后要能读出来。
	if err := st.SetSetting(store.SettingTrayDisabled, "1"); err != nil {
		t.Fatal(err)
	}
	if st.GetSetting(store.SettingTrayDisabled) != "1" {
		t.Fatal("关掉驻留之后读不回来，设置页会一直显示开着")
	}
}

// 没有桌面外壳时，设置页那几个方法不能 panic。
//
// 真实运行时 sh 一定注入了，但服务是绑定给前端的，
// 任何一次没注入（比如以后加了别的入口）都会变成整个设置页白屏。
func TestShellMethodsWithoutShell(t *testing.T) {
	svc := &TopicService{}

	if v := svc.AppShell(); v.Resident {
		t.Fatal("没有外壳时不该报告驻留")
	}
	if err := svc.SetResident(true); err == nil {
		t.Fatal("没有外壳时应返回错误，而不是静默成功")
	}
}

// 关窗的每一次判定。这两条分支各对应一个真实的坑：
// 该销毁时拦了 → 用户关不掉程序，只剩活动监视器；
// 该藏起时放行 → 点个红叉把定时刷新一起关掉，第二天什么都没有。
func TestOnCloseDecides(t *testing.T) {
	cases := []struct {
		name     string
		resident bool
		tray     bool
		quitting bool
		want     closeAction
	}{
		{"驻留开着、图标在，只藏窗口", true, true, false, closeHide},
		{"用户关了驻留，红叉就是退出", false, true, false, closeDestroy},
		{"托盘没起来时不能拦，否则锁死", true, false, false, closeDestroy},
		{"正在退出，放行内置流程", true, true, true, closeDestroy},
	}
	for _, c := range cases {
		sh := &shell{resident: c.resident}
		sh.trayShown.Store(c.tray)
		sh.quitting.Store(c.quitting)
		if got := sh.onClose(); got != c.want {
			t.Fatalf("%s：onClose() = %v，应为 %v", c.name, got, c.want)
		}
	}
}
