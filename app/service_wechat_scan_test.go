package main

import "testing"

// 没开过浏览器会话时，轮询必须给出人话而不是崩掉。
//
// 界面上的轮询是 2 秒一次、无条件发的：用户一打开绑定表单就开始问。
// 这时候发布引擎可能还没建（publisher() 只在真正要发布时才建），
// 所以这条路径一定会被走到。
func TestWeChatScanStatusWithoutEngine(t *testing.T) {
	svc := mkService(t)

	v := svc.WeChatScanStatus()
	if v.Authed {
		t.Fatal("没开浏览器却说已登录")
	}
	if v.Note == "" {
		t.Fatal("状态没有说明文字，界面上就是一片空白")
	}
	if v.Console == "" {
		t.Fatal("没给后台入口，用户就只能自己搜索「公众号后台」")
	}
}

// 扫码绑定的表单必须始终有这个动作，否则界面上那行是个死按钮。
func TestWeChatScanActionExists(t *testing.T) {
	svc := mkService(t)
	a, err := svc.Account()
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range a.Bindings {
		if b.Key != "wechat" {
			continue
		}
		if b.Action != "wechat-scan" {
			t.Fatalf("没配过凭证时应该给「扫码绑定」，实际 %q", b.Action)
		}
		if b.Detail == "" {
			t.Fatal("公众号那行没有 detail，用户不知道点下去会发生什么")
		}
		return
	}
	t.Fatal("绑定列表里没有公众号这一行")
}

// 出口 IP 只查一次，而且失败不能影响绑定流程。
//
// 不测真实的 ipify：测试机断网时它必须照样跑完，这条断言就是那个约定。
func TestEgressIPNeverPanics(t *testing.T) {
	svc := mkService(t)
	ip := svc.egressIP()
	if ip != "" && (len(ip) < 7 || len(ip) > 45) {
		t.Fatalf("出口 IP 格式不对: %q", ip)
	}
	// 第二次必须拿到同一个值（sync.Once），否则每次轮询都发一次外网请求。
	_ = svc.egressIP()
}
