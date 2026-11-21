package pipeline

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"rongsoft/miniwork/app/internal/source"
	"rongsoft/miniwork/app/internal/store"
)

// 首启动填完人设后只重跑模型这一步，绝不能再去取一次数。
// 这条断言的现实意义是配额：每多取一次就是一次真实扣费。
func TestRescoreDoesNotFetchAgain(t *testing.T) {
	st := newStore(t)
	if _, err := Run(context.Background(), st, staticSource{hotList()}, nil,
		Input{Niche: "职场", Limit: 10, MinScore: 50}, nil); err != nil {
		t.Fatal(err)
	}

	// 这里传 nil source 是刻意的：Rescore 不应该用到它，
	// 一旦哪天实现里误接回 fetch，这个测试会直接 panic 而不是悄悄多抓一次。
	f := &fakeAI{content: aiJSON()}
	res, err := Rescore(context.Background(), st, f, "职场", 50, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.AIScored != 2 {
		t.Fatalf("应重算 2 条，实际 %d", res.AIScored)
	}
	if len(res.Topics) == 0 {
		t.Fatal("重算后应带回列表")
	}
}

// 记忆生效的最直接证据：两次相同的候选，只因为人设不同，模型分就不同。
func TestRescoreUsesProfile(t *testing.T) {
	st := newStore(t)
	if _, err := Run(context.Background(), st, staticSource{hotList()}, nil,
		Input{Niche: "职场", Limit: 10, MinScore: 50}, nil); err != nil {
		t.Fatal(err)
	}

	f := &fakeAI{content: aiJSON()}
	if _, err := Rescore(context.Background(), st, f, "职场", 50, nil); err != nil {
		t.Fatal(err)
	}
	before := f.last
	if before == "" {
		t.Fatal("没有发出提示词")
	}

	if err := st.SaveProfile(store.Profile{
		Niche: "职场", Positioning: "二线城市HR，专写职场潜规则",
		Audience: "工作3-8年想跳槽又不敢的白领",
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := Rescore(context.Background(), st, f, "职场", 50, nil); err != nil {
		t.Fatal(err)
	}
	if f.last == before {
		t.Fatal("填了人设后提示词应该变化，实际完全相同")
	}
}

// 模型挂了也不能让重算把分数清零或把列表清空。
func TestRescoreDegrades(t *testing.T) {
	st := newStore(t)
	if _, err := Run(context.Background(), st, staticSource{hotList()}, nil,
		Input{Niche: "职场", Limit: 10, MinScore: 50}, nil); err != nil {
		t.Fatal(err)
	}

	f := &fakeAI{err: errors.New("connection reset")}
	res, err := Rescore(context.Background(), st, f, "职场", 50, nil)
	if err != nil {
		t.Fatalf("模型失败不应让重算报错: %v", err)
	}
	if res.AINote == "" {
		t.Fatal("降级时必须给出一句说明")
	}
	if len(res.Topics) == 0 {
		t.Fatal("降级后列表不能空")
	}
}

// 库里没有待定选题时直接返回，不要发一次空请求去花配额。
func TestRescoreEmptyStore(t *testing.T) {
	st := newStore(t)
	f := &fakeAI{content: aiJSON()}
	res, err := Rescore(context.Background(), st, f, "职场", 50, nil)
	if err != nil {
		t.Fatal(err)
	}
	if f.calls != 0 {
		t.Fatalf("空库不应调用模型，实际 %d 次", f.calls)
	}
	if res.AIScored != 0 {
		t.Fatalf("空库不该有分数，实际 %d", res.AIScored)
	}
}

// 手工加入的选题没有 hotspot 来源，不能拿空 id 去问模型。
func TestRescoreSkipsManualTopics(t *testing.T) {
	st := newStore(t)
	if _, err := st.SaveTopic(store.Topic{
		ID: "manual-1", Title: "自己找的选题", Niche: "职场",
		Score: 60, Status: "draft", CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	f := &fakeAI{content: aiJSON()}
	res, err := Rescore(context.Background(), st, f, "职场", 50, nil)
	if err != nil {
		t.Fatal(err)
	}
	if f.calls != 0 {
		t.Fatalf("只有手工选题时不该调模型，实际 %d 次", f.calls)
	}
	if res.AIScored != 0 {
		t.Fatalf("手工选题不该被打 AI 分")
	}
}

// 没有任何模型时，填完人设也必须让分数当场变——这是首启动三问唯一的即时反馈。
// 如果只有接上模型才生效，用户填完什么变化都看不到，第二次就不会再填。
func TestRescoreAppliesPrefsWithoutAI(t *testing.T) {
	st := newStore(t)
	list := []source.Hotspot{{
		ID: "law1", Platform: "douyin", Title: "被裁员后我申请劳动仲裁，最后拿到2N赔偿",
		Heat: 200000, PublishedAt: time.Now().Add(-1 * time.Hour),
	}}
	if _, err := Run(context.Background(), st, staticSource{list}, nil,
		Input{Niche: "职场", Limit: 10, MinScore: 50}, nil); err != nil {
		t.Fatal(err)
	}

	before, err := st.TopDraft(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != 1 || before[0].Score < 50 {
		t.Fatalf("先决条件不成立：应有 1 条 50 分以上的选题，实际 %+v", before)
	}

	if err := st.SaveProfile(store.Profile{Niche: "职场", Avoid: "不碰裁员赔偿的法律细节"}); err != nil {
		t.Fatal(err)
	}

	// aiClient 传 nil：这正是首启动时用户还没配模型的默认状态。
	if _, err := Rescore(context.Background(), st, nil, "职场", 50, nil); err != nil {
		t.Fatal(err)
	}

	after, err := st.TopDraft(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 1 {
		t.Fatalf("重算不该丢内容，实际 %d 条", len(after))
	}
	if after[0].Score >= before[0].Score {
		t.Fatalf("命中禁忌后分数应下降，前 %d 后 %d", before[0].Score, after[0].Score)
	}
	if !strings.Contains(after[0].ScoreReason, "法律") {
		t.Fatalf("理由应说明是哪条禁忌，实际 %q", after[0].ScoreReason)
	}
}
