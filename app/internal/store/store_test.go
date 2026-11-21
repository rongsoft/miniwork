package store

import (
	"path/filepath"
	"testing"
	"time"

	"rongsoft/miniwork/app/internal/source"
	"rongsoft/miniwork/app/internal/topic"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("打开库失败: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestSchemaVersionApplied(t *testing.T) {
	st := newTestStore(t)
	v, err := st.SchemaVersion()
	if err != nil {
		t.Fatal(err)
	}
	if v != schemaVersion {
		t.Fatalf("schema 版本应为 %d，实际 %d", schemaVersion, v)
	}
}

func TestSaveHotspotsIsIdempotent(t *testing.T) {
	st := newTestStore(t)
	now := time.Now()
	list := []source.Hotspot{
		{ID: "a1", Platform: "xiaohongshu", Title: "标题一", Heat: 100, PublishedAt: now, FetchedAt: now},
		{ID: "a2", Platform: "douyin", Title: "标题二", Heat: 200, PublishedAt: now, FetchedAt: now},
	}

	n1, err := st.SaveHotspots(list, "职场")
	if err != nil {
		t.Fatal(err)
	}
	if n1 != 2 {
		t.Fatalf("首次应新增 2 条，实际 %d", n1)
	}

	n2, err := st.SaveHotspots(list, "职场")
	if err != nil {
		t.Fatal(err)
	}
	if n2 != 0 {
		t.Fatalf("重复抓取不应新增，实际 %d", n2)
	}

	total, err := st.CountHotspots()
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 {
		t.Fatalf("库内应仍为 2 条，实际 %d", total)
	}
}

func TestSetStatusWritesEventAndRejectsIllegal(t *testing.T) {
	st := newTestStore(t)
	now := time.Now()
	th := Topic{
		ID: "t-1", Title: "测试选题", Platform: "xiaohongshu", Niche: "职场",
		Score: 80, Source: "hotspot", Status: topic.Draft, CreatedAt: now,
	}
	if _, err := st.SaveTopic(th); err != nil {
		t.Fatal(err)
	}

	// 合法：draft -> picked
	if err := st.SetStatus("t-1", topic.Picked); err != nil {
		t.Fatalf("应允许收下: %v", err)
	}

	// 非法：draft -> published。连「这条我要写」都没决定就发布出去，
	// 不可能是真事，多半是点错了。（picked / writing 直达 published 是合法的：
	// 没排期当天就发，是创作者最常走的路。）
	if err := st.SetStatus("t-1", topic.Draft); err != nil {
		t.Fatalf("退回待定应允许: %v", err)
	}
	if err := st.SetStatus("t-1", topic.Published); err == nil {
		t.Fatal("非法转移必须报错")
	}

	var events int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM topic_event WHERE topic_id = ?`, "t-1").Scan(&events); err != nil {
		t.Fatal(err)
	}
	if events != 2 {
		t.Fatalf("只有合法转移该留痕，期望 2 条事件，实际 %d", events)
	}
}

func TestSaveTopicDoesNotResetStatus(t *testing.T) {
	st := newTestStore(t)
	now := time.Now()
	th := Topic{ID: "t-2", Title: "重新打分不覆盖状态", Status: topic.Draft, CreatedAt: now}
	if _, err := st.SaveTopic(th); err != nil {
		t.Fatal(err)
	}
	if err := st.SetStatus("t-2", topic.Picked); err != nil {
		t.Fatal(err)
	}

	// 下一次抓取会重新打分并调用 SaveTopic，此时不能把状态打回 draft。
	th.Score = 99
	if _, err := st.SaveTopic(th); err != nil {
		t.Fatal(err)
	}

	list, err := st.ListByStatus(topic.Picked, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("状态应保持不变，期望 1 条已收下，实际 %d", len(list))
	}
	if list[0].Score != 99 {
		t.Fatalf("分数应被更新为 99，实际 %d", list[0].Score)
	}
}

func TestTopDraftOrdersByScore(t *testing.T) {
	st := newTestStore(t)
	now := time.Now()
	for i, s := range []int{40, 90, 60} {
		if _, err := st.SaveTopic(Topic{
			ID: string(rune('a' + i)), Title: "t", Score: s, Status: topic.Draft, CreatedAt: now,
		}); err != nil {
			t.Fatal(err)
		}
	}
	list, err := st.TopDraft(3)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 || list[0].Score != 90 || list[2].Score != 40 {
		t.Fatalf("应按分数降序，实际 %+v", list)
	}
}
