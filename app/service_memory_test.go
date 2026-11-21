package main

import (
	"path/filepath"
	"testing"
	"time"

	"rongsoft/miniwork/app/internal/score"
	"rongsoft/miniwork/app/internal/store"
	"rongsoft/miniwork/app/internal/topic"
)

// 这个场景在真实用户身上很常见：引导走完了但一句人设都没填，人设表是空的，
// 而库里已经攒了一批某个赛道的选题。
//
// 此时记忆页必须和打分口径一致——赛道词不能出现在「学到的偏好」里，
// 因为打分时它们是明确排除的（赛道相关那 40 分就是它们）。
// 一旦不一致，用户会按这份清单去删本该由赛道负责的词，删完发现「怎么还是这样」。
func TestMemoryExcludesNicheWordsWhenProfileEmpty(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	now := time.Now()
	// 三条收下的标题：都含「周报」（职场赛道里的子话题，不在赛道词表），
	// 也都含「领导」「汇报」（赛道词表里的词）。
	//
	// 这样的构造才验得出东西：结果必须学会「周报」这个子话题，
	// 同时把「领导」「汇报」排掉。若三条里的词全在赛道表里，
	// 「不含赛道词」这条断言会因为根本没学到任何词而空转通过。
	for i, title := range []string{
		"领导让我写周报，汇报时这样说",
		"周报怎么写才不像流水账",
		"周报里加这三行，领导不再挑刺",
	} {
		id := title[:2] + string(rune('a'+i))
		if _, err := st.SaveTopic(store.Topic{
			ID: id, Title: title, Platform: "xiaohongshu", Niche: "职场",
			Score: 80, Status: topic.Draft, CreatedAt: now,
		}); err != nil {
			t.Fatal(err)
		}
		if err := st.SetStatus(id, topic.Picked); err != nil {
			t.Fatal(err)
		}
	}

	svc := &TopicService{st: st}
	mv, err := svc.Memory()
	if err != nil {
		t.Fatal(err)
	}

	if mv.Filled {
		t.Fatal("人设没填，Filled 应为 false")
	}

	if !hasLearnt(mv, "周报") {
		t.Fatalf("应学到子话题「周报」，得到 %+v", mv.Learnt.Words)
	}

	for _, w := range mv.Learnt.Words {
		for _, nicheWord := range score.NicheWords("职场") {
			if w.Text == nicheWord {
				t.Fatalf("赛道词「%s」不该出现在偏好里（它在赛道分里已经算过）", w.Text)
			}
		}
	}
}

// 屏蔽之后，Memory 返回的清单里必须立刻没有这个词。
// 这条验的是「界面显示的就是实际生效的」——
// 如果 Memory 读的是没过滤的数据，用户点了 × 却发现词还在，
// 他会以为按钮坏了，然后不再信任这一整块。
func TestMemoryHidesBlockedWord(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "m2.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	now := time.Now()
	for i, title := range []string{
		"领导说我的周报太敷衍",
		"周报怎么写才不像流水账",
		"周报里加这三行，领导不再挑刺",
	} {
		id := title[:2] + string(rune('a'+i))
		if _, err := st.SaveTopic(store.Topic{
			ID: id, Title: title, Platform: "xiaohongshu", Niche: "职场",
			Score: 80, Status: topic.Draft, CreatedAt: now,
		}); err != nil {
			t.Fatal(err)
		}
		if err := st.SetStatus(id, topic.Picked); err != nil {
			t.Fatal(err)
		}
	}

	svc := &TopicService{st: st}
	mv, err := svc.Memory()
	if err != nil {
		t.Fatal(err)
	}
	if !hasLearnt(mv, "周报") {
		t.Fatalf("屏蔽前应学到「周报」，得到 %+v", mv.Learnt.Words)
	}

	if err := svc.BlockLearntWord("周报"); err != nil {
		t.Fatal(err)
	}

	mv, err = svc.Memory()
	if err != nil {
		t.Fatal(err)
	}
	if hasLearnt(mv, "周报") {
		t.Fatalf("屏蔽后清单里不该还有「周报」，得到 %+v", mv.Learnt.Words)
	}
	if len(mv.Blocked) != 1 || mv.Blocked[0] != "周报" {
		t.Fatalf("屏蔽表应记下这个词，得到 %v", mv.Blocked)
	}
}

func hasLearnt(mv MemoryView, text string) bool {
	for _, w := range mv.Learnt.Words {
		if w.Text == text {
			return true
		}
	}
	return false
}
