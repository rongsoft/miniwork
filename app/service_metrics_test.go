package main

import (
	"path/filepath"
	"testing"

	"rongsoft/miniwork/app/internal/publish"
	"rongsoft/miniwork/app/internal/store"
	"rongsoft/miniwork/app/internal/topic"
)

// 分享链接带参数、平台接口不带，靠 id 才能对上。
func TestURLNoteKey(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://www.xiaohongshu.com/explore/68a1b2c3d4e5?xsec_token=AB&xsec_source=pc_share", "68a1b2c3d4e5"},
		{"https://www.xiaohongshu.com/discovery/item/68a1b2c3d4e5", "68a1b2c3d4e5"},
		{"https://www.douyin.com/video/7300000000000000000", "7300000000000000000"},
		{"https://xhslink.com/abc", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := urlNoteKey(c.in); got != c.want {
			t.Fatalf("%q → %q，应该是 %q", c.in, got, c.want)
		}
	}
}

// 平台标题常带话题标签，台账里没有；只差这些的时候必须能对上，
// 否则每一次「一键同步」都会报一堆「没匹配上」。
func TestNormTitle(t *testing.T) {
	cases := []struct{ a, b string }{
		{"一个人也能跑通的自媒体工作台", "一个人也能跑通的自媒体工作台 #效率工具"},
		{"标题：今天 试试 这个", "标题:今天试试这个"},
		{"Hello, World!", "hello world"},
	}
	for _, c := range cases {
		if normTitle(c.a) != normTitle(c.b) {
			t.Fatalf("应该视为同一标题：%q vs %q", c.a, c.b)
		}
	}
	// 内容不同就不能归一成一样，否则会把两条笔记的数据混在一起。
	if normTitle("选题 A") == normTitle("选题 B") {
		t.Fatal("不同标题不该归一成同一个 key")
	}
}

// 匹配是这一步最容易出错的地方：挂错人比没数据难查得多。
func TestMatchMetrics(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	mk := func(id, title, platform string) {
		if _, err := st.SaveTopic(store.Topic{ID: id, Title: title, Platform: platform, Status: topic.Published}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.SaveTopic(store.Topic{ID: "t1", Title: "一个人也能跑通的自媒体工作台"}); err != nil {
		t.Fatal(err)
	}
	mk("t2", "第二条笔记", "xiaohongshu")
	mk("t3", "抖音的作品", "douyin")

	// p1 有链接；p2 没链接，只能靠标题；p3 是抖音的，不能被小红书的数据污染。
	p1, _ := st.AddPublish(store.PublishRecord{
		TopicID: "t1", Platform: "xiaohongshu",
		URL: "https://www.xiaohongshu.com/explore/n1", PublishedAt: "2026-11-10",
	})
	p2, _ := st.AddPublish(store.PublishRecord{
		TopicID: "t2", Platform: "xiaohongshu", PublishedAt: "2026-11-11",
	})
	p3, _ := st.AddPublish(store.PublishRecord{
		TopicID: "t3", Platform: "douyin", PublishedAt: "2026-11-12",
	})

	svc := &TopicService{st: st}
	metrics := []publish.NoteMetric{
		{Key: "n1", Title: "改过的标题", URL: "https://www.xiaohongshu.com/explore/n1", Views: 1200, Likes: 30, Collects: 10, Comments: 4},
		{Key: "n2", Title: "第二条笔记 #日常", Views: 800, Likes: 12, Collects: -1, Comments: -1},
		// 平台上有、台账里没有的一条：不能硬塞给最像的那条。
		{Key: "n9", Title: "别人家的笔记", Views: 9999},
		// 同一条被返回两次（分页重叠）：只用第一次。
		{Key: "n1", Title: "改过的标题", URL: "https://www.xiaohongshu.com/explore/n1", Views: 1, Likes: 1},
	}

	patches, unmatched, err := svc.matchMetrics("xiaohongshu", metrics)
	if err != nil {
		t.Fatal(err)
	}
	if unmatched != 1 {
		t.Fatalf("应该只有 1 条没匹配上，实际 %d", unmatched)
	}
	if len(patches) != 2 {
		t.Fatalf("应该产出 2 条补丁，实际 %d: %+v", len(patches), patches)
	}

	byID := map[string]store.MetricPatch{}
	for _, p := range patches {
		byID[p.ID] = p
	}
	if p, ok := byID[p1]; !ok || p.Views != 1200 {
		t.Fatalf("链接匹配失败: %+v", patches)
	}
	// 重复的那条（Views=1）必须被忽略，不能覆盖成 1。
	if byID[p1].Views == 1 {
		t.Fatal("分页重叠的旧数据覆盖了新数据")
	}
	if p, ok := byID[p2]; !ok || p.Views != 800 {
		t.Fatalf("标题匹配失败（带话题标签的标题应该能对上）: %+v", patches)
	}
	// 平台没给收藏/评论 → -1 → 写回时跳过，不能变成 0。
	if byID[p2].Collects != -1 || byID[p2].Comments != -1 {
		t.Fatalf("未取到的项应该保持 -1: %+v", byID[p2])
	}
	if _, ok := byID[p3]; ok {
		t.Fatal("抖音的记录被小红书的数据匹配上了")
	}
}

// 写回后：数变了、来源标成 auto、时间记下了。
func TestApplyMetricsMarksSource(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	id, _ := st.AddPublish(store.PublishRecord{Platform: "xiaohongshu", PublishedAt: "2026-11-10"})
	n, err := st.ApplyMetrics([]store.MetricPatch{{
		ID: id, Views: 500, Likes: 20, Collects: -1, Comments: -1,
		URL: "https://www.xiaohongshu.com/explore/n1",
	}})
	if err != nil || n != 1 {
		t.Fatalf("写回失败 n=%d err=%v", n, err)
	}

	recs, err := st.ListPublish(10)
	if err != nil {
		t.Fatal(err)
	}
	r := recs[0]
	if r.Views != 500 || r.Likes != 20 {
		t.Fatalf("数没写进去: %+v", r)
	}
	if r.Collects != -1 {
		t.Fatalf("平台没给的项不该被写成 %d", r.Collects)
	}
	if r.MetricsSrc != "auto" {
		t.Fatalf("来源应该是 auto，实际 %q", r.MetricsSrc)
	}
	if r.MetricsAt == "" {
		t.Fatal("取数时间没记下来")
	}
	if r.URL != "https://www.xiaohongshu.com/explore/n1" {
		t.Fatalf("空链接没补上: %q", r.URL)
	}
}

// 用户自己填的链接不能被平台的链接覆盖——他填的可能是个短链或带参数的好分享版。
func TestApplyMetricsKeepsManualURL(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "u.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	id, _ := st.AddPublish(store.PublishRecord{
		Platform: "xiaohongshu", URL: "https://xhslink.com/abc", PublishedAt: "2026-11-10",
	})
	if _, err := st.ApplyMetrics([]store.MetricPatch{{
		ID: id, Views: 10, Likes: 1, Collects: 0, Comments: 0,
		URL: "https://www.xiaohongshu.com/explore/n1",
	}}); err != nil {
		t.Fatal(err)
	}
	recs, _ := st.ListPublish(10)
	if recs[0].URL != "https://xhslink.com/abc" {
		t.Fatalf("手填的链接被覆盖了: %q", recs[0].URL)
	}
}

// 手填之后来源要变成 manual，界面才不会把一个可能抄错的数
// 标成「来自平台」。
func TestUpdateMetricsMarksManual(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "mm.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	id, _ := st.AddPublish(store.PublishRecord{Platform: "xiaohongshu", PublishedAt: "2026-11-10"})
	if err := st.UpdateMetrics(id, 100, -1, -1, -1); err != nil {
		t.Fatal(err)
	}
	recs, _ := st.ListPublish(10)
	if recs[0].MetricsSrc != "manual" {
		t.Fatalf("手填应该标 manual，实际 %q", recs[0].MetricsSrc)
	}
}
