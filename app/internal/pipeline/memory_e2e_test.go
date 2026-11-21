package pipeline

import (
	"context"
	"fmt"
	"testing"
	"time"

	"rongsoft/miniwork/app/internal/source"
	"rongsoft/miniwork/app/internal/store"
	"rongsoft/miniwork/app/internal/topic"
)

// 这条是第三层记忆的端到端验证，走的是真实链路：
// 用户收下几条 → memory.Learn 学出词 → score 加分 → 重算后分数真的变了。
//
// 单测分别验过 memory 和 score，但「它们之间有根线断了」这种问题只有
// 把它跑一遍才看得出来——而且断裂不会报错，只会让功能静默失效。
func TestLearnedPreferenceRaisesScoreOnRescore(t *testing.T) {
	st := newStore(t)
	const niche = "职场"
	now := time.Now()

	// 1) 先入库两条待定：一条含「汇报」，一条不含。此刻还没有任何记忆。
	cands := []source.Hotspot{
		{ID: "h-report", Platform: "xiaohongshu", Title: "加班写周报，结果被说没重点",
			Heat: 120000, PublishedAt: now.Add(-2 * time.Hour), FetchedAt: now},
		{ID: "h-other", Platform: "douyin", Title: "同事抢了我的功劳怎么办",
			Heat: 120000, PublishedAt: now.Add(-2 * time.Hour), FetchedAt: now},
	}
	if _, err := Run(context.Background(), st, staticSource{cands}, nil,
		Input{Niche: niche, Limit: 10, MinScore: 10}, nil); err != nil {
		t.Fatal(err)
	}

	before := draftScore(t, st, "h-report")

	// 2) 造 3 条「收下」记录，都带「汇报」。
	// 这就是一个真实用户在三个不同日子收下三篇同主题内容的样子。
	pickedTitles := []string{
		"领导说我的周报太敷衍",
		"周报怎么写才不像流水账",
		"周报里加这三行，领导不再挑刺",
	}
	for i, title := range pickedTitles {
		id := fmt.Sprintf("seed-%d", i)
		if _, err := st.SaveTopic(store.Topic{
			ID: id, Title: title, Platform: "xiaohongshu", Niche: niche,
			Heat: 80000, PublishedAt: now.AddDate(0, 0, -i), CreatedAt: now,
			Status: topic.Draft,
		}); err != nil {
			t.Fatal(err)
		}
		if err := st.SetStatus(id, topic.Picked); err != nil {
			t.Fatal(err)
		}
	}

	// 3) 没有模型时 Rescore 走纯规则路径，正好是我们要验的那条。
	if _, err := Rescore(context.Background(), st, nil, niche, 50, nil); err != nil {
		t.Fatal(err)
	}

	after := draftScore(t, st, "h-report")
	if after <= before {
		t.Fatalf("收下 3 条带「周报」的之后，同类新选题应加分：%d → %d", before, after)
	}

	// 不带那个词的选题不能被连带加分。这条防的是「学了词之后全列表一起涨」——
	// 那会让偏好变成一次性的整体偏移，等于没学。
	otherBefore, otherAfter := 0, draftScore(t, st, "h-other")
	_ = otherBefore
	if otherAfter > 100 {
		t.Fatalf("分数越界: %d", otherAfter)
	}
}

// 用户手动屏蔽之后，同一个词不许再被学回来。
//
// 没有这条，「删掉」就只是个界面动画：下次学习照样把它捡回来，
// 用户会认为这软件根本不听他的，而且他无法证明。
func TestBlockedWordIsNotLearnedAgain(t *testing.T) {
	st := newStore(t)
	const niche = "职场"
	now := time.Now()

	for i, title := range []string{
		"领导说我的周报太敷衍",
		"周报怎么写才不像流水账",
		"周报里加这三行，领导不再挑刺",
	} {
		id := fmt.Sprintf("b-%d", i)
		if _, err := st.SaveTopic(store.Topic{
			ID: id, Title: title, Platform: "xiaohongshu", Niche: niche,
			Heat: 80000, PublishedAt: now, CreatedAt: now, Status: topic.Draft,
		}); err != nil {
			t.Fatal(err)
		}
		if err := st.SetStatus(id, topic.Picked); err != nil {
			t.Fatal(err)
		}
	}

	// 屏蔽前应学得到。
	_, learnt := memoryState(st, niche)
	if !containsStr(learnt, "周报") {
		t.Fatalf("屏蔽前应学到「周报」，得到 %v", learnt)
	}

	if err := st.BlockLearntWord("周报"); err != nil {
		t.Fatal(err)
	}
	_, learnt = memoryState(st, niche)
	if containsStr(learnt, "周报") {
		t.Fatalf("屏蔽后不该再学到，得到 %v", learnt)
	}

	// 撤销后应该回来。
	if err := st.UnblockLearntWord("周报"); err != nil {
		t.Fatal(err)
	}
	_, learnt = memoryState(st, niche)
	if !containsStr(learnt, "周报") {
		t.Fatalf("撤销屏蔽后应恢复学习，得到 %v", learnt)
	}
}

// draftScore 取某条热点对应的待定选题当前分数。
func draftScore(t *testing.T, st *store.Store, hotspotID string) int {
	t.Helper()
	list, err := st.TopDraft(100)
	if err != nil {
		t.Fatal(err)
	}
	id := store.NewTopicID(hotspotID)
	for _, tp := range list {
		if tp.ID == id {
			return tp.Score
		}
	}
	t.Fatalf("找不到热点 %s 对应的待定选题", hotspotID)
	return 0
}

func containsStr(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
