package pipeline

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"rongsoft/miniwork/app/internal/ai"
	"rongsoft/miniwork/app/internal/source"
	"rongsoft/miniwork/app/internal/store"
)

// fakeAI 记录调用次数和最后一次提示词，用于验证「记忆有没有真的送进去」。
type fakeAI struct {
	content string
	err     error
	calls   int
	last    string
}

func (f *fakeAI) Name() string { return "fake" }
func (f *fakeAI) Complete(_ context.Context, _, user string) (string, error) {
	f.calls++
	f.last = user
	if f.err != nil {
		return "", f.err
	}
	return f.content, nil
}

type staticSource struct{ list []source.Hotspot }

func (s staticSource) Name() string { return "static" }
func (s staticSource) Fetch(context.Context, source.Query) ([]source.Hotspot, error) {
	return s.list, nil
}

func hotList() []source.Hotspot {
	now := time.Now()
	return []source.Hotspot{
		{ID: "h1", Platform: "xiaohongshu", Title: "领导突然对你客气，多半是涨薪的信号", Heat: 120000, PublishedAt: now.Add(-2 * time.Hour), FetchedAt: now},
		{ID: "h2", Platform: "douyin", Title: "面试被问期望薪资，这样答多拿2000", Heat: 90000, PublishedAt: now.Add(-6 * time.Hour), FetchedAt: now},
	}
}

func aiJSON() string {
	return `[{"id":"h1","fit":92,"reason":"和他写过的裁员话题同一个切口"},` +
		`{"id":"h2","fit":40,"reason":"太像通用求职教程，没差异化"}]`
}

func TestAIRefineBlendsScores(t *testing.T) {
	st := newStore(t)
	f := &fakeAI{content: aiJSON()}

	res, err := Run(context.Background(), st, staticSource{hotList()}, f,
		Input{Niche: "职场", Limit: 10, MinScore: 50}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.AIScored != 2 {
		t.Fatalf("应复核 2 条，实际 %d", res.AIScored)
	}

	// 合成公式是 0.6*模型 + 0.4*规则，两条都要能对上。
	list, _ := st.ListByStatus("draft", 10)
	byID := map[string]store.Topic{}
	for _, tp := range list {
		byID[tp.HotspotID] = tp
	}
	h1 := byID["h1"]
	if h1.AIScore != 92 {
		t.Fatalf("模型分应原样存下，实际 %d", h1.AIScore)
	}
	if !strings.Contains(h1.ScoreReason, "同一个切口") {
		t.Fatalf("理由应换成模型给的，实际「%s」", h1.ScoreReason)
	}
	// 92 的模型分不可能得出低于 55 的最终分（规则分也不低）。
	if h1.Score <= 55 {
		t.Fatalf("高分项被合成压得过低: %d", h1.Score)
	}
	if h2 := byID["h2"]; h2.AIScore != 40 || h2.Score >= h1.Score {
		t.Fatalf("模型给了 40 的项不应排在前面: h2=%d h1=%d", h2.Score, h1.Score)
	}
}

func TestAIRefineSeesMemory(t *testing.T) {
	st := newStore(t)
	if err := st.SaveProfile(store.Profile{
		Niche: "职场", Positioning: "二线城市的HR", Audience: "工作3-8年白领",
		Avoid: "不碰赔偿法律细节",
	}); err != nil {
		t.Fatal(err)
	}
	// 一条历史决定：记忆的第二层来自用户行为，不靠手填。
	now := time.Now()
	if _, err := st.SaveTopic(store.Topic{
		ID: "t-old", Title: "被裁员那天我才明白公司不是家", Niche: "职场",
		Score: 70, Status: "draft", CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetStatus("t-old", "picked"); err != nil {
		t.Fatal(err)
	}

	f := &fakeAI{content: aiJSON()}
	if _, err := Run(context.Background(), st, staticSource{hotList()}, f,
		Input{Niche: "职场", Limit: 10, MinScore: 50}, nil); err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{"二线城市的HR", "工作3-8年白领", "不碰赔偿法律细节", "被裁员那天我才明白公司不是家"} {
		if !strings.Contains(f.last, want) {
			t.Fatalf("提示词里缺少记忆「%s」:\n%s", want, f.last)
		}
	}
}

func TestAIRefineDegradesWithoutBreakingRun(t *testing.T) {
	st := newStore(t)
	f := &fakeAI{err: ai.ErrQuotaExceeded}

	res, err := Run(context.Background(), st, staticSource{hotList()}, f,
		Input{Niche: "职场", Limit: 10, MinScore: 50}, nil)
	if err != nil {
		t.Fatalf("模型额度用完不该让整条流水线失败: %v", err)
	}
	if res.Saved == 0 || len(res.Topics) == 0 {
		t.Fatal("降级后仍应产出规则分的选题")
	}
	if !strings.Contains(res.AINote, "额度") {
		t.Fatalf("应说明为什么没用上模型，实际「%s」", res.AINote)
	}
	for _, tp := range res.Topics {
		if tp.AIScore != -1 {
			t.Fatalf("没走过模型应保持 -1，实际 %d", tp.AIScore)
		}
	}
}

func TestAIRefineReportsBrokenResponse(t *testing.T) {
	st := newStore(t)
	f := &fakeAI{content: "抱歉，我不能完成这个请求。"}

	res, err := Run(context.Background(), st, staticSource{hotList()}, f,
		Input{Niche: "职场", Limit: 10, MinScore: 50}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.AINote == "" {
		t.Fatal("模型返回无法解析时必须给出提示，不能静默退回")
	}
}

func TestAIRefineUsesCacheOnRerun(t *testing.T) {
	st := newStore(t)
	f := &fakeAI{content: aiJSON()}
	in := Input{Niche: "职场", Limit: 10, MinScore: 50}

	if _, err := Run(context.Background(), st, staticSource{hotList()}, f, in, nil); err != nil {
		t.Fatal(err)
	}
	if f.calls != 1 {
		t.Fatalf("首次应调用 1 次，实际 %d", f.calls)
	}

	// 同一批内容、同一份记忆、同一个客户端 → 提示词完全相同，必须命中缓存。
	res, err := Run(context.Background(), st, staticSource{hotList()}, f, in, nil)
	if err != nil {
		t.Fatal(err)
	}
	if f.calls != 1 {
		t.Fatalf("重跑应命中缓存不重复计费，实际调用 %d 次", f.calls)
	}
	if res.AIScored != 2 {
		t.Fatalf("缓存命中也要写回 AI 分，实际 %d", res.AIScored)
	}
}

func TestNoAIClientKeepsRuleOnly(t *testing.T) {
	st := newStore(t)
	res, err := Run(context.Background(), st, staticSource{hotList()}, nil,
		Input{Niche: "职场", Limit: 10, MinScore: 50}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.AIScored != 0 || res.AINote != "" {
		t.Fatalf("没配模型时应静默使用规则分，实际 %+v", res)
	}
	if len(res.Topics) == 0 {
		t.Fatal("规则分路径必须照常出选题")
	}
}

func TestAIErrorDoesNotMarkStale(t *testing.T) {
	st := newStore(t)
	f := &fakeAI{err: errors.New("boom")}
	res, err := Run(context.Background(), st, staticSource{hotList()}, f,
		Input{Niche: "职场", Limit: 10, MinScore: 50}, nil)
	if err != nil {
		t.Fatal(err)
	}
	// 模型失败和数据源失败是两件事，不能混成一个「数据陈旧」标志。
	if res.Stale {
		t.Fatal("模型失败不该把数据标成陈旧")
	}
}
