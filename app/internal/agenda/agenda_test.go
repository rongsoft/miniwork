package agenda

import (
	"testing"
	"time"

	"rongsoft/miniwork/app/internal/store"
	"rongsoft/miniwork/app/internal/topic"
)

func day(s string) time.Time {
	t, _ := time.Parse(store.DateLayout, s)
	return t
}

func TestBuildEmpty(t *testing.T) {
	d := Build(Input{Now: day("2026-11-15")})
	if d.Total != 0 {
		t.Fatalf("空输入不该有 item，得到 %d", d.Total)
	}
	// 全空时不能写「暂无待办」这种催人的话。
	if d.Headline != "今天没有非做不可的事" {
		t.Fatalf("headline 不对: %q", d.Headline)
	}
}

// 逾期必须排在今天要发之前：承诺过日子的事，比「今天新排的」更急。
func TestOverdueBeforeDue(t *testing.T) {
	d := Build(Input{
		Now: day("2026-11-15"),
		Due: []store.Topic{
			{ID: "a", Title: "今天要发的", Status: topic.Scheduled, PlanDate: "2026-11-15"},
		},
		Overdue: []store.Topic{
			{ID: "b", Title: "昨天就该发的", Status: topic.Scheduled, PlanDate: "2026-11-14"},
		},
	})

	if len(d.Items) != 2 {
		t.Fatalf("应有 2 条，得到 %d", len(d.Items))
	}
	if d.Items[0].Kind != KindOverdue {
		t.Fatalf("第一条应是逾期，得到 %s", d.Items[0].Kind)
	}
	if d.Items[0].Days != 1 {
		t.Fatalf("逾期天数应为 1，得到 %d", d.Items[0].Days)
	}
	if d.Overdue != 1 || d.Due != 1 {
		t.Fatalf("计数不对: overdue=%d due=%d", d.Overdue, d.Due)
	}
}

// 逾期提示要说清「本来哪天发、拖了几天」，只说「已逾期」等于没说。
func TestOverdueHintHasDates(t *testing.T) {
	d := Build(Input{
		Now:     day("2026-11-15"),
		Overdue: []store.Topic{{ID: "x", Status: topic.Scheduled, PlanDate: "2026-11-12"}},
	})
	if d.Items[0].Hint != "排的 11-12，拖了 3 天" {
		t.Fatalf("hint 不对: %q", d.Items[0].Hint)
	}
}

// 待决定要限量：列十条等于没列，用户会全部划过去。
func TestDecideLimit(t *testing.T) {
	var drafts []store.Topic
	for i := 0; i < 10; i++ {
		drafts = append(drafts, store.Topic{ID: string(rune('a' + i)), Score: 90, Status: topic.Draft})
	}
	d := Build(Input{Now: day("2026-11-15"), Decide: drafts})
	if d.Decide != DecideLimit {
		t.Fatalf("待决定应封顶 %d 条，得到 %d", DecideLimit, d.Decide)
	}
	if len(d.Items) != DecideLimit {
		t.Fatalf("item 数应等于 %d，得到 %d", DecideLimit, len(d.Items))
	}
}

func TestHeadlineCombines(t *testing.T) {
	d := Build(Input{
		Now:     day("2026-11-15"),
		Overdue: []store.Topic{{ID: "b", Status: topic.Scheduled, PlanDate: "2026-11-14"}},
		Due:     []store.Topic{{ID: "a", Status: topic.Scheduled, PlanDate: "2026-11-15"}},
	})
	want := "1 条过了排期没发，1 条今天要发"
	if d.Headline != want {
		t.Fatalf("headline 应为 %q，得到 %q", want, d.Headline)
	}
}

func TestStuckHint(t *testing.T) {
	d := Build(Input{
		Now: day("2026-11-15"),
		Stuck: []store.Aging{
			{Topic: store.Topic{ID: "s", Status: topic.Writing}, Days: 4},
		},
	})
	if d.Items[0].Hint != "写作中停了 4 天" {
		t.Fatalf("hint 不对: %q", d.Items[0].Hint)
	}
}
