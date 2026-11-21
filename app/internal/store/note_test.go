package store

import "testing"

// 台账从「记一条发布」到「打完星写完复盘」是同一个流程的两段。
// 这里把整条走完，重点验三件容易错的事：
// 指标未回填仍是 -1、复盘三件套能回转、统计只数该数的。
func TestPublishReviewRoundTrip(t *testing.T) {
	st := newTestStore(t)

	id, err := st.AddPublish(PublishRecord{
		TopicID: "t-1", Platform: "xiaohongshu", PublishedAt: "2026-11-10",
		Views: 999, Likes: 999, Collects: 999, Comments: 999, // 故意传进来，必须被忽略
	})
	if err != nil {
		t.Fatal(err)
	}

	recs, err := st.ListPublish(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 {
		t.Fatalf("应有 1 条台账，实际 %d", len(recs))
	}
	// AddPublish 强制写 -1：新记的发布不可能已经有数据，
	// 让调用方自己记得传 -1，早晚有一处漏传。
	if r := recs[0]; r.Views != -1 || r.Likes != -1 || r.Collects != -1 || r.Comments != -1 {
		t.Fatalf("新记录的指标应全是 -1，实际 %d/%d/%d/%d", r.Views, r.Likes, r.Collects, r.Comments)
	}
	if recs[0].Rating != 0 {
		t.Fatalf("新记录不该有星级，实际 %d", recs[0].Rating)
	}

	// 只回填两项，另两项应保持 -1（「没看」≠「是 0」）。
	if err := st.UpdateMetrics(id, 1200, 34, -1, -1); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveNoteReview(id, 4, "  前三行留人  ", " 标题先给结果 "); err != nil {
		t.Fatal(err)
	}

	recs, err = st.ListPublish(10)
	if err != nil {
		t.Fatal(err)
	}
	r := recs[0]
	if r.Views != 1200 || r.Likes != 34 {
		t.Fatalf("回填的两个数不对: %d/%d", r.Views, r.Likes)
	}
	if r.Collects != -1 || r.Comments != -1 {
		t.Fatalf("没填的项应保持 -1，实际 %d/%d", r.Collects, r.Comments)
	}
	if r.Rating != 4 {
		t.Fatalf("星级应存下来，实际 %d", r.Rating)
	}
	// 存的字符串要 trim：界面上前后带空格不容易看出来，
	// 但沉淀到「可复用经验」列表里会变成奇怪的缩进。
	if r.Analysis != "前三行留人" || r.Lesson != "标题先给结果" {
		t.Fatalf("复盘文本没存对: %q / %q", r.Analysis, r.Lesson)
	}

	totals, err := st.PublishStats()
	if err != nil {
		t.Fatal(err)
	}
	want := PublishTotals{
		Total: 1, Views: 1200, Likes: 34, Collects: 0, Filled: 1,
		Rated: 1, StarSum: 4, Lessons: 1, Pending: 0,
	}
	if totals != want {
		t.Fatalf("统计不对\n实际 %+v\n期望 %+v", totals, want)
	}
}

func TestSaveNoteReviewRejectsBadRating(t *testing.T) {
	st := newTestStore(t)
	id, err := st.AddPublish(PublishRecord{TopicID: "t-1", PublishedAt: "2026-11-10"})
	if err != nil {
		t.Fatal(err)
	}
	// 星级只有 1–5，0 表示撤销。传 6 或负数必须被拒——
	// 静默接受会让平均星被一个不存在的刻度带偏。
	for _, bad := range []int{-1, 6, 99} {
		if err := st.SaveNoteReview(id, bad, "", ""); err == nil {
			t.Fatalf("星级 %d 应被拒", bad)
		}
	}
	if err := st.SaveNoteReview("不存在", 3, "", ""); err == nil {
		t.Fatal("不存在的 id 应报错，否则用户以为存上了")
	}
}

// 撤销评分（从 5 星改回没评）是真实动作：点错了星得能退回去。
func TestSaveNoteReviewCanClearRating(t *testing.T) {
	st := newTestStore(t)
	id, err := st.AddPublish(PublishRecord{TopicID: "t-1", PublishedAt: "2026-11-10"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SaveNoteReview(id, 5, "写了点东西", ""); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveNoteReview(id, 0, "", ""); err != nil {
		t.Fatal(err)
	}
	recs, _ := st.ListPublish(1)
	if recs[0].Rating != 0 {
		t.Fatalf("撤销后星级应为 0，实际 %d", recs[0].Rating)
	}
	totals, _ := st.PublishStats()
	if totals.Rated != 0 || totals.StarSum != 0 {
		t.Fatalf("撤销后不应计入评分统计: rated=%d sum=%d", totals.Rated, totals.StarSum)
	}
}

// 台账里有没填数据的记录时，Pending 不能把它们算成「待复盘」——
// 那些还在发布台那边，界面会把它们分开显示。
func TestPublishStatsPendingOnlyCountsFilled(t *testing.T) {
	st := newTestStore(t)
	empty, _ := st.AddPublish(PublishRecord{TopicID: "t-1", PublishedAt: "2026-11-10"})
	if _, err := st.AddPublish(PublishRecord{TopicID: "t-2", PublishedAt: "2026-11-11"}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateMetrics(empty, 500, 10, 2, 1); err != nil {
		t.Fatal(err)
	}

	totals, err := st.PublishStats()
	if err != nil {
		t.Fatal(err)
	}
	if totals.Total != 2 || totals.Filled != 1 || totals.Pending != 1 {
		t.Fatalf("统计不对: total=%d filled=%d pending=%d", totals.Total, totals.Filled, totals.Pending)
	}
}
