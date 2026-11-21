package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"rongsoft/miniwork/app/internal/store"
)

// 账号是「机器码即身份」：第一次打开就该有一行，用户什么都不用填。
// 这条测试锁的是「自动建档」这个承诺——一旦有人在 Account() 里加了
// 「没名字就报错」，客户第一次打开就会被拦住。
func TestAccountAutoRegister(t *testing.T) {
	svc := mkService(t)

	a, err := svc.Account()
	if err != nil {
		t.Fatalf("Account: %v", err)
	}
	if a.Code == "" {
		t.Fatal("机器码是空的，等于没有身份")
	}
	if strings.Count(a.Code, "-") != 3 {
		t.Fatalf("机器码格式不对（应为 XXXX-XXXX-XXXX-XXXX）: %q", a.Code)
	}
	if a.Complete {
		t.Fatal("刚建档就算「资料填全」了，提示永远不会出现")
	}
	// 再问一次必须是同一个账号，不能每次调用都新建一行。
	again, err := svc.Account()
	if err != nil || again.Code != a.Code {
		t.Fatalf("账号不幂等: %q vs %q (err=%v)", a.Code, again.Code, err)
	}

	// 绑定状态要能反映真实情况：这个服务注入了脚本化模型，所以 AI 应该是「已连接」。
	// 反过来「没模型时必须显示未绑定」由下面那条用例管——两种状态都要有测试，
	// 只测一种的话，写死 Ready=true 也能过。
	var sawAI bool
	for _, b := range a.Bindings {
		if b.Key == "ai" {
			sawAI = true
			if !b.Ready || b.Status != "已连接" {
				t.Fatalf("有模型却说没连接: %+v", b)
			}
		}
	}
	if !sawAI {
		t.Fatalf("绑定列表里没有模型: %+v", a.Bindings)
	}
}

// 没有模型时，绑定那一行必须是「未绑定」，并告诉用户去填 Key。
// 和上一条成对：一条测有、一条测没有。
func TestAccountBindingWithoutAI(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "noai.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := &TopicService{st: st, srcLabel: "示例数据"}

	a, err := svc.Account()
	if err != nil {
		t.Fatalf("Account: %v", err)
	}
	for _, b := range a.Bindings {
		if b.Key != "ai" {
			continue
		}
		if b.Ready || b.Status != "未绑定" {
			t.Fatalf("没有模型却显示已连接: %+v", b)
		}
		if !strings.Contains(b.Detail, "Key") {
			t.Fatalf("未绑定时要告诉用户去填 Key，实际: %q", b.Detail)
		}
		return
	}
	t.Fatalf("绑定列表里没有模型: %+v", a.Bindings)
}

func TestUpdateAccount(t *testing.T) {
	svc := mkService(t)
	if _, err := svc.Account(); err != nil {
		t.Fatal(err)
	}

	v, err := svc.UpdateAccount("张三", "138 0013 8000")
	if err != nil {
		t.Fatalf("UpdateAccount: %v", err)
	}
	if v.Name != "张三" || v.Phone != "13800138000" {
		t.Fatalf("资料没存住或没清洗空格: %+v", v)
	}
	if !v.Complete {
		t.Fatal("姓名和手机号都有，应该算填全")
	}
	if v.Unbound != countUnbound(v) {
		t.Fatalf("未绑定数量对不上: %d", v.Unbound)
	}

	// 明显不是号码的输入要挡住，而不是存进去让售后永远打不通。
	if _, err := svc.UpdateAccount("张三", "我的微信号是 abc"); err == nil {
		t.Fatal("非号码的手机号应该报错")
	}
	// 留空是合法的：客户可以不填。
	if v, err := svc.UpdateAccount("张三", ""); err != nil || v.Complete {
		t.Fatalf("清空手机号应成功且不算填全: %+v err=%v", v, err)
	}
}

func countUnbound(v AccountView) int {
	n := 0
	for _, b := range v.Bindings {
		if !b.Ready {
			n++
		}
	}
	return n
}

// 从表格里复制手机号会带上防科学计数法的那对单引号。
// 库里真出现过 '13900000001'，账号页照着显示，用户以为是我们存坏了。
func TestPhoneQuotesTidied(t *testing.T) {
	svc := mkService(t)
	if _, err := svc.Account(); err != nil {
		t.Fatal(err)
	}
	v, err := svc.UpdateAccount("刘荣", "'13900000001'")
	if err != nil {
		t.Fatalf("带引号的号码应该被清洗后接受: %v", err)
	}
	if v.Phone != "13900000001" {
		t.Fatalf("引号没去掉: %q", v.Phone)
	}
	// 姓名同理：粘贴带引号是常事。
	v, err = svc.UpdateAccount("\"刘荣\"", "")
	if err != nil || v.Name != "刘荣" {
		t.Fatalf("姓名里的引号没去掉: %+v err=%v", v, err)
	}
}

// 库里已经存下的脏数据要在读的时候自愈，不能只挡新输入——
// 那台机器上的号已经存成带引号的了，光挡新的等于不修。
func TestAccountTidiesLegacyRow(t *testing.T) {
	svc := mkService(t)
	a, err := svc.Account()
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.st.SaveAccountProfile(a.Code, "'刘荣'", "'13900000001'", time.Now()); err != nil {
		t.Fatal(err)
	}
	v, err := svc.Account()
	if err != nil {
		t.Fatal(err)
	}
	if v.Name != "刘荣" || v.Phone != "13900000001" {
		t.Fatalf("旧脏数据没清洗: %+v", v)
	}
	// 再读一次，库里也应该是干净的（自愈写过一次）。
	again, _ := svc.Account()
	if again.Phone != "13900000001" {
		t.Fatalf("自愈没写回库: %+v", again)
	}
}

// 绑定行必须带一个能执行的出口。
// 「本机没 Chrome」时给「扫码登录」是骗人的：点了只会报错，
// 用户会以为登录坏了，而不是知道自己缺个浏览器。
func TestBindingActions(t *testing.T) {
	svc := mkService(t)
	a, err := svc.Account()
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range a.Bindings {
		if b.Action == "" {
			t.Fatalf("%s 这一行没有动作，界面上就是一个点不动的状态", b.Key)
		}
		if b.Key == "wechat" {
			// 公众号有两种状态：没配过就扫码（自动填 AppID），配过就改凭证。
			// 哪个都不许给「填凭证」以外的手填表单当第一入口——
			// 用户就是嫌那个表单复杂才要求扫码的。
			if b.Action != "wechat-scan" && b.Action != "wechat-config" {
				t.Fatalf("公众号的动作应该是扫码或改凭证，实际 %q", b.Action)
			}
			if !b.Ready && b.Action != "wechat-scan" {
				t.Fatalf("没配过凭证时应该给「扫码绑定」，实际 %q", b.Action)
			}
		}
		if b.Key == "ai" && b.Action != "setup-ai" {
			t.Fatalf("模型那一行应该是「去填 Key」，实际 %q", b.Action)
		}
		// 测试机没有 Chrome，浏览器平台必须落到「装个 Chrome」，
		// 而不是给一个点了就失败的「扫码登录」。
		if (b.Key == "xiaohongshu" || b.Key == "douyin") && b.Action == "login" {
			// 装了 Chrome 的机器上这条不成立，用 Available 判断，
			// 而不是写死「一定有/一定没有浏览器」。
			if !hasBrowser(t) {
				t.Fatalf("%s 在没浏览器的机器上还给了「扫码登录」", b.Key)
			}
		}
	}
}

// hasBrowser 问引擎本身，别在测试里复制一遍路径表。
func hasBrowser(t *testing.T) bool {
	t.Helper()
	return mkService(t).PublishStatus().BrowserReady
}
