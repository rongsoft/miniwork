package store

import (
	"strings"
	"testing"
	"time"
)

func TestSaveDraftThenRead(t *testing.T) {
	st := newTestStore(t)

	id, err := st.SaveDraft(Draft{
		TopicID: "t-1", Platform: "xiaohongshu", Format: "long",
		Title: "标题", Body: "正文第一段\n\n正文第二段",
		Tags: []string{"职场", "求职"}, Note: "角度",
	})
	if err != nil {
		t.Fatal(err)
	}
	if id == "" {
		t.Fatal("新建稿件必须返回 id")
	}

	d, err := st.DraftByID(id)
	if err != nil {
		t.Fatal(err)
	}
	if d.Title != "标题" || len(d.Tags) != 2 {
		t.Fatalf("读回来的稿子不对: %+v", d)
	}
	if d.Edited {
		t.Error("刚生成的稿不该标成已编辑")
	}
}

func TestSaveDraftKeepsCreatedAt(t *testing.T) {
	st := newTestStore(t)
	id, err := st.SaveDraft(Draft{TopicID: "t-1", Title: "初稿", Body: "b"})
	if err != nil {
		t.Fatal(err)
	}
	before, _ := st.DraftByID(id)

	time.Sleep(10 * time.Millisecond)
	if _, err := st.SaveDraft(Draft{
		ID: id, TopicID: "t-1", Title: "改过的稿", Body: "b2", Edited: true,
	}); err != nil {
		t.Fatal(err)
	}
	after, _ := st.DraftByID(id)

	if after.Title != "改过的稿" || !after.Edited {
		t.Fatalf("更新没生效: %+v", after)
	}
	// created_at 是「这一稿什么时候有的」。跟着每次编辑刷新的话，
	// 「最新一稿」就变成了「最后被打开过的一稿」。
	if !after.CreatedAt.Equal(before.CreatedAt) {
		t.Errorf("created_at 不该被编辑刷新: %v -> %v", before.CreatedAt, after.CreatedAt)
	}
	if !after.UpdatedAt.After(before.UpdatedAt) {
		t.Error("updated_at 应当前进")
	}
}

func TestDraftsByTopicNewestFirst(t *testing.T) {
	st := newTestStore(t)
	if _, err := st.SaveDraft(Draft{TopicID: "t-1", Title: "第一稿", Body: "a"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	if _, err := st.SaveDraft(Draft{TopicID: "t-1", Title: "第二稿", Body: "b"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveDraft(Draft{TopicID: "t-2", Title: "别的选题", Body: "c"}); err != nil {
		t.Fatal(err)
	}

	list, err := st.DraftsByTopic("t-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("同一选题应有两稿，实际 %d", len(list))
	}
	if list[0].Title != "第二稿" {
		t.Errorf("最新的应排在前面，实际 %q", list[0].Title)
	}
}

func TestDraftedTopicIDs(t *testing.T) {
	st := newTestStore(t)
	if _, err := st.SaveDraft(Draft{TopicID: "t-1", Title: "a", Body: "b"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveDraft(Draft{TopicID: "t-1", Title: "a2", Body: "b2"}); err != nil {
		t.Fatal(err)
	}

	got, err := st.DraftedTopicIDs()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !got["t-1"] {
		t.Fatalf("同一选题两稿只该算一个已备稿选题，实际 %v", got)
	}
}

func TestDeleteDraftKeepsPublishRecord(t *testing.T) {
	st := newTestStore(t)
	id, _ := st.SaveDraft(Draft{TopicID: "t-1", Title: "a", Body: "b"})
	if _, err := st.AddPublish(PublishRecord{
		TopicID: "t-1", DraftID: id, Platform: "xiaohongshu",
		PublishedAt: "2026-11-15",
	}); err != nil {
		t.Fatal(err)
	}

	if err := st.DeleteDraft(id); err != nil {
		t.Fatal(err)
	}
	// 稿子可以删，但「我发过什么」必须留着——那是台账。
	list, err := st.ListPublish(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("台账不该随稿件一起消失，实际 %d 条", len(list))
	}
}

func TestAddPublishDefaultsToToday(t *testing.T) {
	st := newTestStore(t)
	id, err := st.AddPublish(PublishRecord{TopicID: "t-1", Platform: "douyin"})
	if err != nil {
		t.Fatal(err)
	}
	list, _ := st.ListPublish(10)
	if len(list) != 1 || list[0].ID != id {
		t.Fatal("记录没写进去")
	}
	if list[0].PublishedAt != time.Now().Format(DateLayout) {
		t.Errorf("空日期应默认今天，实际 %q", list[0].PublishedAt)
	}
	// 新建记录的指标必须是 -1（未回填），不能是结构体零值 0：
	// 0 会被当成「真没人看」算进均值。
	if list[0].Views != -1 || list[0].Likes != -1 {
		t.Errorf("新建记录的指标应为未回填 -1，实际 %+v", list[0])
	}
}

func TestUpdateMetricsSentinels(t *testing.T) {
	st := newTestStore(t)
	id, err := st.AddPublish(PublishRecord{TopicID: "t-1", PublishedAt: "2026-11-15"})
	if err != nil {
		t.Fatal(err)
	}

	// 只改 views 和 likes，其余传 -1 表示不动。
	if err := st.UpdateMetrics(id, 1200, 80, -1, -1); err != nil {
		t.Fatal(err)
	}
	list, _ := st.ListPublish(10)
	got := list[0]
	if got.Views != 1200 || got.Likes != 80 {
		t.Fatalf("该改的没改: %+v", got)
	}
	if got.Collects != -1 {
		t.Errorf("传 -1 的项不该被写成 0，实际 %d", got.Collects)
	}

	// 0 是合法数据：真没人收藏。必须能填进去。
	if err := st.UpdateMetrics(id, -1, -1, 0, -1); err != nil {
		t.Fatal(err)
	}
	list, _ = st.ListPublish(10)
	if list[0].Collects != 0 {
		t.Errorf("0 应当能写进去，实际 %d", list[0].Collects)
	}
}

func TestPublishStatsIgnoresUnfilled(t *testing.T) {
	st := newTestStore(t)
	filled, err := st.AddPublish(PublishRecord{TopicID: "t-1", PublishedAt: "2026-11-14"})
	if err != nil {
		t.Fatal(err)
	}
	// 指标只能通过回填写入，新建时一律是未回填。
	if err := st.UpdateMetrics(filled, 100, 10, 3, -1); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddPublish(PublishRecord{TopicID: "t-2", PublishedAt: "2026-11-15"}); err != nil {
		t.Fatal(err)
	}

	totals, err := st.PublishStats()
	if err != nil {
		t.Fatal(err)
	}
	if totals.Total != 2 {
		t.Errorf("总数应含未回填的记录，实际 %d", totals.Total)
	}
	// 未回填的那条按 0 参与求和，不能算成「播放 0」拿去拉低均值，
	// 但它也不该被算成缺失而报错。
	if totals.Views != 100 || totals.Likes != 10 || totals.Collects != 3 {
		t.Errorf("求和口径不对: views=%d likes=%d collects=%d",
			totals.Views, totals.Likes, totals.Collects)
	}
	// 回填条数要在库里数。界面上只列最近 60 条，用它当分母会得到
	// 「55/120」这种自相矛盾的比例。
	if totals.Filled != 1 {
		t.Errorf("只回填了 1 条，实际报 %d", totals.Filled)
	}
}

func TestNewIDUnique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		id := newID("d-")
		if seen[id] {
			t.Fatalf("id 重复: %s", id)
		}
		seen[id] = true
		if !strings.HasPrefix(id, "d-") {
			t.Fatalf("前缀丢了: %s", id)
		}
	}
}
