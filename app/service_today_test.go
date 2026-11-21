package main

import (
	"testing"
	"time"

	"rongsoft/miniwork/app/internal/store"
	"rongsoft/miniwork/app/internal/topic"
)

// 「刷新灵感没变化」这条缺陷的回归测试。
//
// 根因不是一个函数写错，而是三处口径各说各话：列表全局按分数排、
// 「今日新增」拿全部待定顶替、流水线把「重算旧条目」也算成「入选」。
// 所以这里一条一条钉住，改动时任何一条被打回，用户立刻能再看见同一屏。

func addDraft(t *testing.T, svc *TopicService, id, title, platform string, score int, at time.Time) {
	t.Helper()
	if _, err := svc.st.SaveTopic(store.Topic{
		ID: id, Title: title, Platform: platform, Niche: "职场",
		Score: score, Status: topic.Draft, CreatedAt: at,
	}); err != nil {
		t.Fatalf("写入选题失败: %v", err)
	}
}

func TestListPutsTodayBatchFirst(t *testing.T) {
	svc := mkService(t)
	now := time.Now()

	// 三天前那条分更高：全局按分数排的话它会永远压在今天新抓的上面，
	// 用户点完刷新只看到同一屏，就会说「数据没更新」。
	addDraft(t, svc, "t-old", "三天前的高分选题", "xiaohongshu", 95, now.Add(-72*time.Hour))
	addDraft(t, svc, "t-new", "刚刚抓回来的选题", "xiaohongshu", 60, now)

	views, err := svc.List("draft", 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(views) != 2 {
		t.Fatalf("应有 2 条，实际 %d", len(views))
	}
	if views[0].ID != "t-new" {
		t.Fatalf("今天这批应排第一，实际第一是 %s（%s）", views[0].ID, views[0].Title)
	}
	if !views[0].Today {
		t.Fatal("本批次抓回来的条目 today 应为 true")
	}
	if views[1].Today {
		t.Fatal("三天前的条目不该被标成今天")
	}
	if views[1].DayLabel == "" {
		t.Fatal("非今天的条目必须带日期标签，否则用户分不清哪张是旧的")
	}
}

func TestListDedupesSameTitleAcrossPlatforms(t *testing.T) {
	svc := mkService(t)
	now := time.Now()

	// 同一条内容两个平台各发一次，库里是两行，界面上必须只有一张卡。
	addDraft(t, svc, "t-a", "部门要裁员时，第一批走的是哪种人", "xiaohongshu", 70, now.Add(-24*time.Hour))
	addDraft(t, svc, "t-b", "部门要裁员时，第一批走的是哪种人", "douyin", 76, now)

	views, err := svc.List("draft", 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(views) != 1 {
		t.Fatalf("同标题应合成一条，实际 %d 条", len(views))
	}
	if views[0].ID != "t-b" {
		t.Fatalf("应保留今天那条（t-b），实际 %s", views[0].ID)
	}
}

func TestStatsCountsOnlyTodayAsNew(t *testing.T) {
	svc := mkService(t)
	now := time.Now()
	addDraft(t, svc, "t-old", "昨天的选题", "xiaohongshu", 80, now.Add(-30*time.Hour))
	addDraft(t, svc, "t-new1", "今天的第一条", "xiaohongshu", 70, now)
	addDraft(t, svc, "t-new2", "今天的第二条", "douyin", 65, now)

	s, err := svc.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if s.Draft != 3 {
		t.Fatalf("待定总数应为 3，实际 %d", s.Draft)
	}
	if s.Today != 2 {
		t.Fatalf("今日新增应为 2，实际 %d", s.Today)
	}
}

func TestDecideDropsSiblingWithSameTitle(t *testing.T) {
	svc := mkService(t)
	now := time.Now()
	addDraft(t, svc, "t-a", "同一篇内容被两个平台都发了", "xiaohongshu", 70, now)
	addDraft(t, svc, "t-b", "同一篇内容被两个平台都发了", "douyin", 72, now)
	addDraft(t, svc, "t-c", "另一条不相干的选题", "xiaohongshu", 60, now)

	// 界面上只显示去重后的一张卡，用户丢掉它，另一个平台的兄弟也必须走掉，
	// 否则下次刷新它又冒出来，用户会觉得「丢掉根本没生效」。
	if err := svc.Decide("t-b", "drop"); err != nil {
		t.Fatalf("Decide: %v", err)
	}
	left, err := svc.List("draft", 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(left) != 1 || left[0].ID != "t-c" {
		t.Fatalf("同标题兄弟应一并丢掉，剩 %d 条", len(left))
	}
}

func TestSaveTopicReportsCreatedOnlyOnce(t *testing.T) {
	svc := mkService(t)
	t1 := store.Topic{ID: "t-x", Title: "第一次写入", Status: topic.Draft, Score: 60, CreatedAt: time.Now()}

	created, err := svc.st.SaveTopic(t1)
	if err != nil || !created {
		t.Fatalf("首次写入应返回 created=true（created=%v err=%v）", created, err)
	}

	t1.Score = 75
	created, err = svc.st.SaveTopic(t1)
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Fatal("第二次写入是刷新旧条目，不能报成新增——否则界面每天都在报「入选 20 条」")
	}

	got, err := svc.st.TopicByID("t-x")
	if err != nil {
		t.Fatal(err)
	}
	if got.Score != 75 {
		t.Fatalf("刷新分数没写进去，实际 %d", got.Score)
	}
}

func TestDayLabelUsesCalendarDays(t *testing.T) {
	// 用固定的日历日造数据，不用 now-24h：凌晨跑测试时「24 小时前」
	// 会落到前天，测试就变成了看时钟的脸色。
	loc := time.Local
	batch := time.Date(2026, 11, 18, 9, 0, 0, 0, loc)
	cases := []struct {
		at   time.Time
		want string
	}{
		{time.Date(2026, 11, 18, 0, 0, 0, 0, loc), ""},
		{time.Date(2026, 11, 18, 8, 59, 0, 0, loc), ""},
		{time.Date(2026, 11, 17, 23, 59, 0, 0, loc), "昨天"},
		{time.Date(2026, 11, 16, 9, 0, 0, 0, loc), "前天"},
		{time.Date(2026, 11, 10, 9, 0, 0, 0, loc), "11月10日"},
	}
	for _, c := range cases {
		if got := dayLabel(c.at, batch); got != c.want {
			t.Errorf("dayLabel(%v) = %q，期望 %q", c.at, got, c.want)
		}
	}
}

// 今天 0 点之后抓的都算今天这批，不能因为「上一次点刷新是十分钟前」
// 就把它们挤到更早里去。
func TestBatchStartIsStartOfToday(t *testing.T) {
	svc := mkService(t)
	start := svc.batchStart()
	now := time.Now()
	if start.Hour() != 0 || start.Minute() != 0 || start.Second() != 0 {
		t.Fatalf("批次起点应是今天 0 点，实际 %v", start)
	}
	if start.After(now) || now.Sub(start) > 24*time.Hour {
		t.Fatalf("批次起点不在今天之内: %v", start)
	}
}
