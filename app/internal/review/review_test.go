package review

import (
	"testing"
	"time"

	"rongsoft/miniwork/app/internal/store"
)

func day(s string) time.Time {
	t, _ := time.Parse(store.DateLayout, s)
	return t
}

func TestBuildEmpty(t *testing.T) {
	r := Build(Input{Now: day("2026-11-15"), Days: 7})
	if r.Generated != 0 {
		t.Fatalf("空库应产出 0，得到 %d", r.Generated)
	}
	if r.Headline != "这段时间还没有数据" {
		t.Fatalf("headline 不对: %q", r.Headline)
	}
	// 趋势图必须补齐连续 7 天，前端不该自己补洞。
	if len(r.Trend) != 7 {
		t.Fatalf("趋势应有 7 个点，得到 %d", len(r.Trend))
	}
	if r.Trend[6].Day != "11-15" {
		t.Fatalf("最后一天应是 11-15，得到 %s", r.Trend[6].Day)
	}
}

// 「收了没发」是创作者最痛的问题，headline 要优先说它。
func TestHeadlinePickedNotPublished(t *testing.T) {
	r := Build(Input{
		Now:     day("2026-11-15"),
		Days:    7,
		Created: 10,
		Counts: map[string]int{
			"picked": 4, "writing": 2, "draft": 4,
		},
	})
	if r.Headline != "收了没发" {
		t.Fatalf("headline 应为「收了没发」，得到 %q", r.Headline)
	}
	if r.Picked != 6 {
		t.Fatalf("收下应为 6（picked+writing），得到 %d", r.Picked)
	}
	found := false
	for _, n := range r.Notes {
		if n == "收了 6 条，一条都没发出去。问题不在选题，在写。" {
			found = true
		}
	}
	if !found {
		t.Fatalf("缺少「收了没发」那条判断，notes=%v", r.Notes)
	}
}

// 只丢不收说明标准比产能高，这条也要说出来。
func TestNoteOnlyDropping(t *testing.T) {
	r := Build(Input{
		Now:     day("2026-11-15"),
		Days:    7,
		Created: 20,
		Counts:  map[string]int{"dropped": 20},
	})
	if r.Headline != "只丢不收" {
		t.Fatalf("headline 应为「只丢不收」，得到 %q", r.Headline)
	}
}

func TestFunnelPercentIsRelativeToGenerated(t *testing.T) {
	r := Build(Input{
		Now:     day("2026-11-15"),
		Days:    7,
		Created: 40,
		Counts:  map[string]int{"published": 10, "draft": 30},
	})
	// 四级都相对「产出」，这样每一级的衰减都看得见。
	if r.Funnel[0].Count != 40 || r.Funnel[0].Pct != 100 {
		t.Fatalf("产出级不对: %+v", r.Funnel[0])
	}
	if r.Funnel[3].Count != 10 || r.Funnel[3].Pct != 25 {
		t.Fatalf("发布级应为 10 条 25%%，得到 %+v", r.Funnel[3])
	}
	if r.ShipRate != 100 {
		t.Fatalf("收下 10 条发 10 条，发布率应 100%%，得到 %d", r.ShipRate)
	}
}

func TestAINotAvailable(t *testing.T) {
	c := buildAI(nil)
	if c.Available || c.Verdict == "" {
		t.Fatalf("没有模型数据时应标为不可用并给一句说明，得到 %+v", c)
	}
}

func TestAIHighKeptHigh(t *testing.T) {
	var pairs []store.AIPair
	for i := 0; i < 10; i++ {
		pairs = append(pairs, store.AIPair{Fit: 85, Status: "picked"})
	}
	c := buildAI(pairs)
	if c.HighTotal != 10 || c.HighKept != 10 {
		t.Fatalf("10 条高分全收，计数不对: %+v", c)
	}
	if c.Verdict != "它挑出的 10 条高分，你收了 10 条——它开始懂你了" {
		t.Fatalf("verdict 不对: %q", c.Verdict)
	}
}

// 模型给高分、用户几乎都不收：问题多半在人设写得太粗，得指出来。
func TestAIHighKeptLow(t *testing.T) {
	var pairs []store.AIPair
	for i := 0; i < 10; i++ {
		st := "dropped"
		if i == 0 {
			st = "picked"
		}
		pairs = append(pairs, store.AIPair{Fit: 90, Status: st})
	}
	c := buildAI(pairs)
	if c.HighKept != 1 {
		t.Fatalf("HighKept 应为 1，得到 %d", c.HighKept)
	}
	if c.Verdict != "它挑的 10 条高分你只收了 1 条——多半是它还不够了解你，去把人设写细点" {
		t.Fatalf("verdict 不对: %q", c.Verdict)
	}
}

func TestAISmallSample(t *testing.T) {
	c := buildAI([]store.AIPair{{Fit: 80, Status: "picked"}})
	if c.Verdict != "模型只给过 1 条高分，样本太少，再攒几天" {
		t.Fatalf("小样本该说样本少，得到 %q", c.Verdict)
	}
}

// 一条选题丢两次再捡回来，收下不能算两次。
func TestTrendCountsPickOnce(t *testing.T) {
	now := day("2026-11-15")
	// 事件落在 15 号白天（now 是零点，直接减小时数会掉到 14 号）。
	events := []store.Event{
		{TopicID: "t1", From: "draft", To: "picked", At: now.Add(9 * time.Hour)},
		{TopicID: "t1", From: "picked", To: "dropped", At: now.Add(10 * time.Hour)},
		{TopicID: "t1", From: "dropped", To: "draft", At: now.Add(11 * time.Hour)},
		{TopicID: "t1", From: "draft", To: "picked", At: now.Add(12 * time.Hour)},
	}
	r := Build(Input{Now: now, Days: 7, Events: events})
	if got := r.Trend[6].Picked; got != 1 {
		t.Fatalf("同一条只算一次收下，得到 %d", got)
	}
}

func TestRegretDaysFromDropEvent(t *testing.T) {
	now := day("2026-11-15")
	events := []store.Event{
		{TopicID: "t1", From: "draft", To: "dropped", At: day("2026-11-10")},
	}
	r := Build(Input{
		Now:     now,
		Days:    30,
		Regrets: []store.Topic{{ID: "t1", Title: "高分题", Score: 88, Platform: "douyin"}},
		Events:  events,
	})
	if len(r.Regrets) != 1 {
		t.Fatalf("应有 1 条遗憾，得到 %d", len(r.Regrets))
	}
	if r.Regrets[0].Days != 5 {
		t.Fatalf("丢了 5 天，得到 %d", r.Regrets[0].Days)
	}
	if r.Regrets[0].Platform != "douyin" {
		t.Fatalf("平台应透传给 service 层，得到 %q", r.Regrets[0].Platform)
	}
}

func TestStuckSortedByDays(t *testing.T) {
	r := Build(Input{
		Now:  day("2026-11-15"),
		Days: 7,
		Stuck: []store.Aging{
			{Topic: store.Topic{ID: "a"}, Days: 3},
			{Topic: store.Topic{ID: "b"}, Days: 9},
		},
	})
	if len(r.Stuck) != 2 || r.Stuck[0].ID != "b" {
		t.Fatalf("卡最久的应排第一，得到 %+v", r.Stuck)
	}
}

// 90 天窗口不能画成 90 根柱子，必须合并。合并后最后一格要是完整的一组。
func TestTrendCoarsensForLongWindow(t *testing.T) {
	r := Build(Input{Now: day("2026-11-15"), Days: 90})
	if len(r.Trend) > 14 {
		t.Fatalf("90 天窗口最多出 14 个点，得到 %d", len(r.Trend))
	}
	if len(r.Trend) == 0 || r.Trend[len(r.Trend)-1].Day != "11-15" {
		t.Fatalf("最后一格应落在今天，得到 %+v", r.Trend)
	}
}

func TestTrendMonthlyUsesThreeDayBuckets(t *testing.T) {
	r := Build(Input{Now: day("2026-11-15"), Days: 30})
	if len(r.Trend) != 10 {
		t.Fatalf("30 天按 3 天一格应出 10 个点，得到 %d", len(r.Trend))
	}
}
