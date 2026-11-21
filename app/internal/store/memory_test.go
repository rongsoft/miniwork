package store

import (
	"testing"
	"time"
)

func TestProfileRoundTrip(t *testing.T) {
	st := newTestStore(t)

	// 没填过时是空值，不是错误。
	p, err := st.Profile()
	if err != nil {
		t.Fatal(err)
	}
	if !p.Empty() {
		t.Fatalf("新库画像应为空，实际 %+v", p)
	}

	in := Profile{
		Niche: "职场", Positioning: "二线城市HR", Audience: "3-8 年白领",
		Tone: "克制", Avoid: "不碰赔偿法律",
	}
	if err := st.SaveProfile(in); err != nil {
		t.Fatal(err)
	}
	out, err := st.Profile()
	if err != nil {
		t.Fatal(err)
	}
	if out.Positioning != in.Positioning || out.Avoid != in.Avoid || out.Empty() {
		t.Fatalf("画像写入后读不一致: %+v", out)
	}
	if out.UpdatedAt.IsZero() {
		t.Fatal("UpdatedAt 应自动填")
	}

	// 再存一次是覆盖，不是新增行。
	if err := st.SaveProfile(Profile{Niche: "理财"}); err != nil {
		t.Fatal(err)
	}
	out2, _ := st.Profile()
	if out2.Niche != "理财" || out2.Positioning != "" {
		t.Fatalf("应整体覆盖，实际 %+v", out2)
	}
	var n int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM profile`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("profile 只能有一行，实际 %d", n)
	}
}

func TestRecentDecisionsSplitsByStatus(t *testing.T) {
	st := newTestStore(t)
	now := time.Now()
	for i, title := range []string{"收下的A", "丢掉的B", "收下的C"} {
		id := string(rune('a' + i))
		if _, err := st.SaveTopic(Topic{ID: id, Title: title, Status: "draft", CreatedAt: now.Add(time.Duration(i) * time.Minute)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.SetStatus("a", "picked"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetStatus("b", "dropped"); err != nil {
		t.Fatal(err)
	}

	d, err := st.RecentDecisions(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Picked) != 1 || d.Picked[0] != "收下的A" {
		t.Fatalf("收下集合不对: %+v", d.Picked)
	}
	if len(d.Dropped) != 1 || d.Dropped[0] != "丢掉的B" {
		t.Fatalf("丢掉集合不对: %+v", d.Dropped)
	}
	// 还在待定的那条不该进记忆——用户还没做决定，那不是偏好。
	for _, v := range append(d.Picked, d.Dropped...) {
		if v == "收下的C" {
			t.Fatal("待定项不应进记忆")
		}
	}
}

func TestUpdateAIScoreKeepsStatus(t *testing.T) {
	st := newTestStore(t)
	if _, err := st.SaveTopic(Topic{ID: "t1", Title: "x", Score: 55, Status: "draft", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetStatus("t1", "picked"); err != nil {
		t.Fatal(err)
	}

	if err := st.UpdateAIScore("t1", 90, 76, "模型理由", ""); err != nil {
		t.Fatal(err)
	}

	list, err := st.ListByStatus("picked", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatal("打分不该改动状态：收下的应还是收下")
	}
	got := list[0]
	if got.AIScore != 90 || got.Score != 76 || got.ScoreReason != "模型理由" {
		t.Fatalf("AI 分写入不对: %+v", got)
	}

	// 理由为空时保留原规则理由，避免模型返回半截把说明清空。
	if err := st.UpdateAIScore("t1", 80, 70, "", ""); err != nil {
		t.Fatal(err)
	}
	list, _ = st.ListByStatus("picked", 10)
	if list[0].ScoreReason != "模型理由" {
		t.Fatalf("空理由不应覆盖已有说明，实际「%s」", list[0].ScoreReason)
	}
	if list[0].AIScore != 80 || list[0].Score != 70 {
		t.Fatalf("分数应更新，实际 %+v", list[0])
	}
}

func TestAICacheRoundTrip(t *testing.T) {
	st := newTestStore(t)
	if _, ok, err := st.AICacheGet("k1"); err != nil || ok {
		t.Fatalf("空缓存应返回 false, 实际 ok=%v err=%v", ok, err)
	}
	if err := st.AICachePut("k1", "relay", "first"); err != nil {
		t.Fatal(err)
	}
	if err := st.AICachePut("k1", "relay", "second"); err != nil {
		t.Fatal(err)
	}
	v, ok, err := st.AICacheGet("k1")
	if err != nil || !ok {
		t.Fatalf("应命中缓存: %v %v", ok, err)
	}
	if v != "second" {
		t.Fatalf("同 key 应覆盖为最后一次结果，实际 %q", v)
	}
}
