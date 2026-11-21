package publish

import (
	"encoding/json"
	"testing"
)

// 平台给的数字有四种形状：接口里的数字、字符串、网页上的「1.2万」、
// 以及带千分位的「1,234」。四种都要能解析，否则换个抓法就全变 0。
func TestParseCount(t *testing.T) {
	cases := []struct {
		in   any
		want int
		ok   bool
	}{
		{float64(1234), 1234, true},
		{float64(0), 0, true},
		{"1234", 1234, true},
		{"0", 0, true},
		{"1.2万", 12000, true},
		{"10w+", 100000, true},
		{"3万", 30000, true},
		{"1.25万", 12500, true},
		{"1.2亿", 120000000, true},
		{"1,234", 1234, true},
		{"5k", 5000, true},
		{"", 0, false},
		{"--", 0, false},
		{"很多", 0, false},
	}
	for _, c := range cases {
		got, ok := parseCount(c.in)
		if ok != c.ok {
			t.Fatalf("%v: ok = %v，应该是 %v", c.in, ok, c.ok)
		}
		if ok && got != c.want {
			t.Fatalf("%v: 解析成 %d，应该是 %d", c.in, got, c.want)
		}
	}
}

// 「没这个数」和「这个数是 0」必须分开。
// 分开的方式是 -1：界面据此显示「未取到」而不是「0 播放」。
func TestCountMissingIsMinusOne(t *testing.T) {
	m := map[string]any{"title": "只有标题"}
	if got := count(m, "view_count", "views"); got != -1 {
		t.Fatalf("缺字段应该是 -1，实际 %d", got)
	}
	m2 := map[string]any{"view_count": float64(0)}
	if got := count(m2, "view_count"); got != 0 {
		t.Fatalf("真的是 0 就该是 0，实际 %d", got)
	}
}

// 小红书真实返回的形状：data.notes[]，字段是 view_count 这一套。
func TestPickNotesXiaohongshu(t *testing.T) {
	raw := []byte(`{
		"code": 0,
		"data": {
			"notes": [
				{"id":"n1","title":"一个人也能跑通","view_count":1200,"liked_count":35,
				 "collected_count":12,"comments_count":3,"time":1763000000},
				{"id":"n2","title":"第二条","view_count":0,"liked_count":0,
				 "collected_count":0,"comments_count":0}
			],
			"total": 2
		}
	}`)
	got := pickNotes(raw)
	if len(got) != 2 {
		t.Fatalf("应该取到 2 条，实际 %d", len(got))
	}
	a := got[0]
	if a.Key != "n1" || a.Title != "一个人也能跑通" {
		t.Fatalf("基本字段错了: %+v", a)
	}
	if a.Views != 1200 || a.Likes != 35 || a.Collects != 12 || a.Comments != 3 {
		t.Fatalf("指标错了: %+v", a)
	}
	if a.PublishedAt != "2025-11-13" {
		t.Fatalf("时间戳没换算对: %q", a.PublishedAt)
	}
	// 第二条全 0 必须保持 0，不能被当成「没取到」。
	if got[1].Views != 0 {
		t.Fatalf("0 播放被改成了 %d", got[1].Views)
	}
}

// 抖音的作品列表：aweme_list，字段是统计对象套在 statistics 里。
// 这一条同时验证「挪了字段不会全崩」——statistics 里的数拿不到时
// 就是 -1，而不是整条丢掉。
func TestPickNotesDouyin(t *testing.T) {
	raw := []byte(`{
		"status_code": 0,
		"aweme_list": [
			{"aweme_id":"a1","desc":"标题在 desc 里","create_time":1763000000,
			 "statistics":{"play_count":5000,"digg_count":80}}
		]
	}`)
	got := pickNotes(raw)
	if len(got) != 1 {
		t.Fatalf("应该取到 1 条，实际 %d", len(got))
	}
	if got[0].Key != "a1" || got[0].Title != "标题在 desc 里" {
		t.Fatalf("基本字段错了: %+v", got[0])
	}
	if got[0].Views != 5000 || got[0].Likes != 80 {
		t.Fatalf("指标错了: %+v", got[0])
	}
	// 嵌套的 statistics 我们没钻进去取收藏/评论，那两项就该是 -1。
	if got[0].Collects != -1 || got[0].Comments != -1 {
		t.Fatalf("没取到的项应该是 -1: %+v", got[0])
	}
}

// 公众号 datacube 的返回：list[]，日期是字符串。
func TestPickNotesWechat(t *testing.T) {
	raw := []byte(`{
		"list": [
			{"ref_date":"2026-11-10","msgid":"10000001","title":"一篇文章",
			 "int_page_read_count":800,"share_count":20,"add_to_fav_count":7}
		]
	}`)
	got := pickNotes(raw)
	if len(got) != 1 {
		t.Fatalf("应该取到 1 条，实际 %d", len(got))
	}
	if got[0].PublishedAt != "2026-11-10" {
		t.Fatalf("日期解析错了: %q", got[0].PublishedAt)
	}
	if got[0].Views != 800 || got[0].Likes != 20 || got[0].Collects != 7 {
		t.Fatalf("指标错了: %+v", got[0])
	}
}

// 接口里混了别的数组时，不能把推荐位 / 配置项当成笔记列表。
func TestPickNotesSkipsNonNotes(t *testing.T) {
	raw := []byte(`{
		"data": {
			"tabs": [{"name":"笔记"},{"name":"合集"}],
			"notes": [{"id":"n1","title":"真笔记","view_count":10}]
		}
	}`)
	got := pickNotes(raw)
	if len(got) != 1 || got[0].Title != "真笔记" {
		t.Fatalf("挑错了: %+v", got)
	}
}

// 返回的不是 JSON（被登录页重定向、被风控拦）→ 一条都不给，不能瞎猜。
func TestPickNotesRejectsHTML(t *testing.T) {
	if got := pickNotes([]byte(`<html><body>请登录</body></html>`)); len(got) != 0 {
		t.Fatalf("HTML 不该解析出笔记，实际 %d 条", len(got))
	}
}

// 时间戳有秒和毫秒两种，差 1000 倍会把日期算到 1970 年。
func TestToDate(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{"2026-11-10", "2026-11-10"},
		{"2026-11-10 12:00:00", "2026-11-10"},
		{float64(1763000000), "2025-11-13"},
		{float64(1763000000000), "2025-11-13"},
		{json.Number("1763000000"), "2025-11-13"},
		{"", ""},
		{float64(0), ""},
	}
	for _, c := range cases {
		if got := toDate(c.in); got != c.want {
			t.Fatalf("%v → %q，应该是 %q", c.in, got, c.want)
		}
	}
}

// 平台不支持取数时要给 ErrNoMetrics，界面才能说人话。
func TestFetchMetricsUnsupportedPlatform(t *testing.T) {
	e := New(Options{DataDir: t.TempDir(), ExecPath: "/nonexistent/chrome"})
	if _, err := e.FetchMetrics("wechat"); err != ErrNoMetrics {
		t.Fatalf("公众号应该走接口那条路（ErrNoMetrics），实际 %v", err)
	}
	if _, err := e.FetchMetrics("不存在的平台"); err == nil {
		t.Fatal("未知平台必须报错")
	}
}
