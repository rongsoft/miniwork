package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"rongsoft/miniwork/app/internal/store"
)

func TestNoteRate(t *testing.T) {
	cases := []struct {
		name              string
		likes, coll, comm int
		views             int
		want              float64
	}{
		{"正常算", 30, 10, 10, 1000, 5},
		{"四舍五入到一位", 1, 0, 0, 300, 0.3},
		// 没有播放数就没有分母。硬算成 100% 会让一条 3 赞的笔记
		// 排到 12 万播放的上面，而后者才是真答案。
		{"没有播放数算不了", 3, 0, 0, -1, -1},
		{"播放为 0 也算不了", 3, 0, 0, 0, -1},
		// 只填了播放、其余没填：按 0 参与，显示 0% 是诚实的，
		// 用户自己能看出另外三项还空着。
		{"未回填的项按 0", -1, -1, -1, 500, 0},
	}
	for _, c := range cases {
		got := noteRate(c.likes, c.coll, c.comm, c.views)
		if got != c.want {
			t.Errorf("%s: 期望 %v，实际 %v", c.name, c.want, got)
		}
	}
}

func TestNoteAge(t *testing.T) {
	now := time.Date(2026, 11, 15, 10, 30, 0, 0, time.Local)
	cases := []struct{ date, want string }{
		{"2026-11-15", "今天发的"},
		{"2026-11-14", "发了 1 天"},
		{"2026-11-10", "发了 5 天"},
		// 写错格式时返回空串，界面上就不显示这一段——
		// 好过显示「发了 0 天」这种假话。
		{"2026/11/10", ""},
		{"", ""},
		// 一年多以前的台账不该再标「发了 400 天」，那已经不能叫复盘的时效了。
		{"2025-01-01", ""},
	}
	for _, c := range cases {
		if got := noteAge(c.date, now); got != c.want {
			t.Errorf("%q: 期望 %q，实际 %q", c.date, c.want, got)
		}
	}
	// 昨晚 23:59 发的，今天早上看应该算「昨天」而不是「0 天」。
	late := time.Date(2026, 11, 15, 9, 0, 0, 0, time.Local)
	if got := noteAge("2026-11-14", late); got != "发了 1 天" {
		t.Errorf("隔夜应算 1 天，实际 %q", got)
	}
}

func note(rate float64, rating int) NoteView {
	return NoteView{Reviewed: rating > 0, Rate: rate, Rating: rating}
}

func TestNoteInsight(t *testing.T) {
	// 两边各要 2 条才下结论：1 条对 1 条的差额多半是运气，
	// 拿噪音当结论会让用户按错的信号改写法。
	thin := noteInsight([]NoteView{note(5, 5), note(1, 1)})
	if thin.Available {
		t.Fatal("样本不足时不该给结论")
	}
	if !strings.Contains(thin.Verdict, "各攒到 2 条") {
		t.Fatalf("结论文案不对: %q", thin.Verdict)
	}

	// 眼光准：高星互动率明显高于低星。
	good := noteInsight([]NoteView{note(8, 5), note(6, 4), note(2, 1), note(1, 2)})
	if !good.Available || !strings.Contains(good.Verdict, "眼光是准的") {
		t.Fatalf("应判定为眼光准，实际 %q", good.Verdict)
	}

	// 反过来：自己觉得好的没人看。这是最该被说出来的一种情况。
	back := noteInsight([]NoteView{note(1, 5), note(2, 4), note(9, 1), note(8, 2)})
	if !back.Available || !strings.Contains(back.Verdict, "不是一回事") {
		t.Fatalf("应判定为背离，实际 %q", back.Verdict)
	}

	// 差不多：说明决定数据的是别的东西，不是内容好坏。
	same := noteInsight([]NoteView{note(5, 5), note(5.5, 4), note(5.2, 1), note(4.8, 2)})
	if !same.Available || !strings.Contains(same.Verdict, "封面、标题或发布时间") {
		t.Fatalf("应判定为无关，实际 %q", same.Verdict)
	}

	// 没打星的、没有分母的都不参与比较，否则「没数据」会被当成 0% 拉低均值。
	mixed := noteInsight([]NoteView{note(8, 0), note(-1, 5), note(9, 5), note(3, 1), note(2, 2)})
	if mixed.GoodN != 1 || mixed.BadN != 2 || mixed.Available {
		t.Fatalf("不该计入无效样本: %+v", mixed)
	}
}

func TestSaveNoteReviewTrimsAndClips(t *testing.T) {
	// 超长文本按字截断，不能按字节——按字节截会切出半个汉字。
	long := strings.Repeat("好", noteLessonMax+50)
	if got := clipRunes("  "+long+"  ", noteLessonMax); len([]rune(got)) != noteLessonMax {
		t.Fatalf("应截到 %d 字，实际 %d", noteLessonMax, len([]rune(got)))
	}
	if got := clipRunes("  前后有空格  ", 100); got != "前后有空格" {
		t.Fatalf("应去掉首尾空白，实际 %q", got)
	}
}

// 这条走真库：SQL、排序、字段回转都过一遍，验的是「界面上能不能拿到正确的数据」，
// 而不只是「函数算得对不对」。
func TestNoteBoardOnRealStore(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "n.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	// 三条：一有数据没星（待复盘）、一有数据有星、一没数据也没星（不该出现在这里）。
	filled, _ := st.AddPublish(store.PublishRecord{TopicID: "t1", Platform: "xiaohongshu", PublishedAt: "2026-11-10"})
	rated, _ := st.AddPublish(store.PublishRecord{TopicID: "t2", Platform: "wechat", PublishedAt: "2026-11-01"})
	if _, err := st.AddPublish(store.PublishRecord{TopicID: "t3", Platform: "douyin", PublishedAt: "2026-11-12"}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateMetrics(filled, 1000, 20, 10, 5); err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateMetrics(rated, 2000, 10, 10, 10); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveNoteReview(rated, 5, "为什么好", "开篇给动作"); err != nil {
		t.Fatal(err)
	}

	svc := &TopicService{st: st}
	board, err := svc.NoteBoard()
	if err != nil {
		t.Fatal(err)
	}
	if len(board.Items) != 2 {
		t.Fatalf("没填数据的不该进复盘列表，实际 %d 条", len(board.Items))
	}
	// 没复盘的排前面：数据已经稳定了，现在不写，过两周就想不起来当时怎么想的。
	if board.Items[0].ID != filled || board.Items[0].Reviewed {
		t.Fatalf("待复盘应排在最前，实际首条 %s reviewed=%v", board.Items[0].ID, board.Items[0].Reviewed)
	}
	if got := board.Items[0].Rate; got != 3.5 {
		t.Fatalf("互动率应为 3.5，实际 %v", got)
	}
	if len(board.Lessons) != 1 || board.Lessons[0].Lesson != "开篇给动作" {
		t.Fatalf("可复用经验没捞出来: %+v", board.Lessons)
	}
	if board.Stats.Pending != 1 || board.Stats.Rated != 1 || board.Stats.Unrated != 1 {
		t.Fatalf("统计不对: %+v", board.Stats)
	}
	if board.Stats.AvgStar != 5 {
		t.Fatalf("平均星应为 5，实际 %v", board.Stats.AvgStar)
	}

	// 存一次复盘后，同一条必须回到「已复盘」那一侧。
	if err := svc.SaveNoteReview(NoteReviewInput{ID: filled, Rating: 3, Lesson: "标题要具体"}); err != nil {
		t.Fatal(err)
	}
	board, err = svc.NoteBoard()
	if err != nil {
		t.Fatal(err)
	}
	if board.Stats.Pending != 0 || board.Stats.Rated != 2 {
		t.Fatalf("存完复盘后统计没跟上: %+v", board.Stats)
	}
	if board.Stats.AvgStar != 4 {
		t.Fatalf("平均星应为 (5+3)/2=4，实际 %v", board.Stats.AvgStar)
	}
}
