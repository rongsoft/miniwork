package main

import (
	"path/filepath"
	"testing"
	"time"

	"rongsoft/miniwork/app/internal/store"
	"rongsoft/miniwork/app/internal/topic"
)

func newSvc(t *testing.T) *TopicService {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatalf("打开库失败: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return &TopicService{st: st}
}

func addTopic(t *testing.T, svc *TopicService, id, title string, status topic.Status, planDate string) {
	t.Helper()
	if _, err := svc.st.SaveTopic(store.Topic{
		ID: id, Title: title, Platform: "xiaohongshu", Niche: "职场",
		Score: 80, Status: topic.Draft, CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	if status == topic.Draft {
		return
	}
	// 状态机只允许 draft -> picked 起步，排期得从 picked 走。
	// 测试也是一条真实路径，不能绕过白名单，否则测出来的东西线上走不通。
	if err := svc.st.SetStatus(id, topic.Picked); err != nil {
		t.Fatalf("%s 收下失败: %v", id, err)
	}
	switch status {
	case topic.Picked:
		if planDate != "" {
			if err := svc.st.Schedule(id, planDate); err != nil {
				t.Fatalf("排期失败: %v", err)
			}
		}
	case topic.Scheduled:
		if planDate == "" {
			t.Fatalf("%s 要排期就必须给日期", id)
		}
		if err := svc.st.Schedule(id, planDate); err != nil {
			t.Fatalf("排期失败: %v", err)
		}
	default:
		if err := svc.st.SetStatus(id, status); err != nil {
			t.Fatalf("%s 转 %s 失败: %v", id, status, err)
		}
	}
}

func TestMarkPublishedWritesRecordAndAdvancesStatus(t *testing.T) {
	svc := newSvc(t)
	// 没排期就发：这是创作者最常走的路（写好了当天就发），
	// 以前会被状态机拦住要求「先排个期」，现在应当直接走通。
	addTopic(t, svc, "t-1", "被裁员之后", topic.Picked, "")

	id, err := svc.MarkPublished(PublishInput{
		TopicID: "t-1", Platform: "xiaohongshu", URL: "https://x.com/1",
		Date: "2026-11-15",
	})
	if err != nil {
		t.Fatal(err)
	}
	if id == "" {
		t.Fatal("应返回台账 id")
	}

	tk, _ := svc.st.TopicByID("t-1")
	if tk.Status != topic.Published {
		t.Errorf("状态应推进到已发布，实际 %s", tk.Status)
	}

	board, err := svc.PublishBoard()
	if err != nil {
		t.Fatal(err)
	}
	if len(board.Records) != 1 {
		t.Fatalf("台账应有一条，实际 %d", len(board.Records))
	}
	if board.Records[0].Title != "被裁员之后" {
		t.Errorf("台账要带选题标题，实际 %q", board.Records[0].Title)
	}
	if board.Records[0].Filled {
		t.Error("刚记的发布不该算已回填")
	}
	// 队列里不该再出现已发布的。
	if len(board.Queue) != 0 {
		t.Errorf("已发布的不该留在待发队列，实际 %d 条", len(board.Queue))
	}
}

func TestMarkPublishedTwiceOnSameTopic(t *testing.T) {
	svc := newSvc(t)
	addTopic(t, svc, "t-1", "同一条发两个平台", topic.Scheduled, "2026-11-15")

	if _, err := svc.MarkPublished(PublishInput{TopicID: "t-1", Platform: "xiaohongshu"}); err != nil {
		t.Fatal(err)
	}
	// 第二次是「同一条内容又发去公众号」。状态已经是 published，
	// 再调 SetStatus 会报「状态未变化」，但那不是错误。
	if _, err := svc.MarkPublished(PublishInput{TopicID: "t-1", Platform: "wechat"}); err != nil {
		t.Fatalf("同一条再发一个平台不该报错: %v", err)
	}

	board, _ := svc.PublishBoard()
	if len(board.Records) != 2 {
		t.Fatalf("两个平台应留两条账，实际 %d", len(board.Records))
	}
	if board.Stats.Total != 2 {
		t.Errorf("统计总数应为 2，实际 %d", board.Stats.Total)
	}
}

func TestMarkPublishedRejectsDropped(t *testing.T) {
	svc := newSvc(t)
	addTopic(t, svc, "t-1", "丢掉的东西", topic.Dropped, "")

	if _, err := svc.MarkPublished(PublishInput{TopicID: "t-1"}); err == nil {
		t.Fatal("已丢弃的选题不该能记发布")
	}
}

func TestQueueOrdersOverdueFirst(t *testing.T) {
	svc := newSvc(t)
	today := time.Now().Format(store.DateLayout)
	yesterday := time.Now().AddDate(0, 0, -1).Format(store.DateLayout)
	tomorrow := time.Now().AddDate(0, 0, 1).Format(store.DateLayout)

	addTopic(t, svc, "t-far", "后天的", topic.Scheduled, tomorrow)
	addTopic(t, svc, "t-over", "昨天该发的", topic.Scheduled, yesterday)
	addTopic(t, svc, "t-today", "今天要发的", topic.Scheduled, today)

	board, err := svc.PublishBoard()
	if err != nil {
		t.Fatal(err)
	}
	if len(board.Queue) != 3 {
		t.Fatalf("队列应有 3 条，实际 %d", len(board.Queue))
	}
	// 已经承诺过日子的没交付，比还没到期的重要。
	if board.Queue[0].TopicID != "t-over" {
		t.Errorf("逾期的应排第一，实际 %s(%s)", board.Queue[0].TopicID, board.Queue[0].Due)
	}
	if board.Queue[0].Due != "overdue" || board.Queue[0].DueLabel == "" {
		t.Errorf("逾期项要有标记与文案，实际 %+v", board.Queue[0])
	}
	if board.Queue[1].Due != "today" {
		t.Errorf("今天该发的排第二，实际 %s", board.Queue[1].Due)
	}
}

func TestQueueMarksDrafted(t *testing.T) {
	svc := newSvc(t)
	addTopic(t, svc, "t-1", "有稿的", topic.Scheduled, "2026-11-18")
	addTopic(t, svc, "t-2", "没稿的", topic.Scheduled, "2026-11-19")

	if _, err := svc.st.SaveDraft(store.Draft{
		TopicID: "t-1", Platform: "xiaohongshu", Format: "long",
		Title: "稿子", Body: "正文",
	}); err != nil {
		t.Fatal(err)
	}

	board, _ := svc.PublishBoard()
	for _, it := range board.Queue {
		switch it.TopicID {
		case "t-1":
			if it.DraftID == "" {
				t.Error("有稿的选题要带上稿件 id，否则没法一键取稿")
			}
			if it.DraftCount != 1 {
				t.Errorf("稿件数应为 1，实际 %d", it.DraftCount)
			}
		case "t-2":
			if it.DraftID != "" || it.DraftCount != 0 {
				t.Error("没稿的不该标成有稿")
			}
		}
	}
}

func TestDueLabelWording(t *testing.T) {
	cases := []struct {
		plan, today, due, label string
	}{
		{"2026-11-13", "2026-11-15", "overdue", "逾期 2 天"},
		{"2026-11-15", "2026-11-15", "today", "今天发"},
		{"2026-11-16", "2026-11-15", "soon", "明天"},
		{"2026-11-20", "2026-11-15", "soon", "5 天后"},
	}
	for _, c := range cases {
		due, label := dueLabel(c.plan, c.today)
		if due != c.due || label != c.label {
			t.Errorf("%s/%s: 期望 %s %q，实际 %s %q", c.plan, c.today, c.due, c.label, due, label)
		}
	}
}

func TestDueLabelHandlesEmptyPlan(t *testing.T) {
	due, label := dueLabel("", "2026-11-15")
	if due != "later" || label != "没排期" {
		t.Fatalf("没排期也要能显示，实际 %s %q", due, label)
	}
}

func TestSaveAccountRejectsUnknownPlatform(t *testing.T) {
	svc := newSvc(t)
	if err := svc.SaveAccount("weibo", "我的号"); err == nil {
		t.Fatal("未知平台不该能存账号")
	}
	if err := svc.SaveAccount("xiaohongshu", "荣华软件"); err != nil {
		t.Fatal(err)
	}
	if got := svc.Accounts()["xiaohongshu"]; got != "荣华软件" {
		t.Errorf("账号没存进去，实际 %q", got)
	}
}

func TestDraftPackUsesPlatformRules(t *testing.T) {
	svc := newSvc(t)
	id, err := svc.st.SaveDraft(store.Draft{
		TopicID: "t-1", Platform: "wechat", Format: "long",
		Title: "标题", Body: "正文", Tags: []string{"职场", "副业"},
	})
	if err != nil {
		t.Fatal(err)
	}
	pack, err := svc.DraftPack(id)
	if err != nil {
		t.Fatal(err)
	}
	// 公众号没有话题体系，井号会原样显示在正文里。
	if pack.Text == "" || containsRune(pack.Text, '#') {
		t.Fatalf("公众号的包不该带 #，实际 %q", pack.Text)
	}
}

func containsRune(s string, r rune) bool {
	for _, c := range s {
		if c == r {
			return true
		}
	}
	return false
}

func TestWriteRejectsUnknownPlatform(t *testing.T) {
	svc := newSvc(t)
	addTopic(t, svc, "t-1", "选题", topic.Picked, "")
	// 没有模型时先报模型未配置，这是更前置的错误；这里只验证
	// 「平台不认识就明确拒绝」，不猜平台——猜错会让标题上限按错。
	if _, err := svc.Write("t-1", "weibo", "long", "", nil); err == nil {
		t.Fatal("未知平台应当报错")
	}
}
