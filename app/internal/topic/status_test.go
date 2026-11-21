package topic

import "testing"

func TestCanAllowsWhitelistedTransitions(t *testing.T) {
	allowed := [][2]Status{
		{Draft, Picked},
		{Draft, Dropped},
		{Picked, Writing},
		{Picked, Scheduled},
		// 没排期直接发是常态：稿子写好了当天就发，回来标记时
		// 不该被要求补一个形式上的排期日。
		{Picked, Published},
		{Writing, Published},
		{Writing, Scheduled},
		{Scheduled, Published},
		{Scheduled, Writing},
		{Dropped, Draft},
	}
	for _, p := range allowed {
		if !Can(p[0], p[1]) {
			t.Errorf("应当允许 %s -> %s", p[0], p[1])
		}
	}
}

func TestCanRejectsIllegalTransitions(t *testing.T) {
	denied := [][2]Status{
		{Draft, Published},   // 不能跳过写作直接发布
		{Draft, Writing},     // 不能跳过收下
		{Published, Draft},   // 已发布不可回退
		{Dropped, Published}, // 丢弃后不能直接发布
	}
	for _, p := range denied {
		if Can(p[0], p[1]) {
			t.Errorf("不应允许 %s -> %s", p[0], p[1])
		}
	}
}

func TestCheckSameStatusReturnsError(t *testing.T) {
	if err := Check(Picked, Picked); err == nil {
		t.Fatal("相同状态应返回错误，避免写空事件")
	}
}

func TestLabelIsHumanReadable(t *testing.T) {
	if Draft.Label() != "待定" {
		t.Errorf("Draft 的中文应为「待定」，实际 %q", Draft.Label())
	}
	if !Published.Terminal() || !Dropped.Terminal() {
		t.Error("已发布与已丢弃应当被标记为终态")
	}
}
