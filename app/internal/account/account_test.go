package account

import (
	"regexp"
	"testing"
)

// 机器码是这个产品里唯一稳定不变的东西：台账、归档、将来的授权都挂在它上面。
// 两条硬要求——格式固定、同一次运行内稳定。
func TestCode(t *testing.T) {
	c := Code()
	if !regexp.MustCompile(`^[0-9A-F]{4}(-[0-9A-F]{4}){3}$`).MatchString(c) {
		t.Fatalf("机器码格式不对: %q", c)
	}
	if again := Code(); again != c {
		t.Fatalf("同一个进程里两次取值不一致: %q vs %q", c, again)
	}
}

// 虚拟机/工控板会返回全 0 或全 F，那种值当主键会把一堆机器并成同一个账号。
func TestUsable(t *testing.T) {
	bad := []string{"", "  ", "00000000-0000-0000-0000-000000000000", "FFFFFFFF-FFFF-FFFF-FFFF-FFFFFFFFFFFF", "----"}
	for _, s := range bad {
		if usable(s) {
			t.Errorf("应该判为不可用: %q", s)
		}
	}
	if !usable("4C4C4544-0044-3510-8051-CAC04F4D4E32") {
		t.Error("正常的硬件 UUID 被误判成不可用")
	}
}

func TestFormat(t *testing.T) {
	if got := format("4A7F19C28E305B61"); got != "4A7F-19C2-8E30-5B61" {
		t.Fatalf("format = %q", got)
	}
	// 长度不对时原样返回，不要 panic 也不要补零——补零会造出一个假身份。
	if got := format("ABC"); got != "ABC" {
		t.Fatalf("异常长度应原样返回，得到 %q", got)
	}
}
