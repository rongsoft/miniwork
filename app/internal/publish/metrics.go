package publish

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

// 抓平台侧的真实数据。
//
// 为什么值得写这一段：播放、点赞这些数只有平台知道，让用户一条条手抄
// 不只是麻烦——他会抄错、会漏、会干脆不抄，于是复盘页永远是空的，
// 整个「用数据校准判断」的设计就废了。我们已经有一台带登录态的浏览器，
// 这些数就在它打开的那个页面里。
//
// 三条底线：
//  1. 抓不到就说抓不到，绝不写 0。0 是「这条真的没人看」，不是「没抓到」。
//  2. 只读不写：这里只 GET，不发任何会改动平台数据的请求。
//  3. 平台改版必然发生，所以接口用候选列表而不是写死一个；
//     全都不通时错误信息要能让人判断「是改版了」还是「没登录」。

// ErrNoMetrics 表示这个平台不支持自动取数。
//
// 单独一个错误是为了界面能说「这个平台只能手填」，
// 而不是把一句技术性的失败糊到用户脸上——那会让他以为程序坏了。
var ErrNoMetrics = errors.New("这个平台不支持自动取数")

// ErrMetricsShape 表示接口通了，但返回的东西里找不到笔记列表。
//
// 这是「平台改版」的典型信号，和「没登录」「网络不通」必须分开报：
// 前者我们只能等更新，后者用户自己能解决。
var ErrMetricsShape = errors.New("平台返回的数据里没有笔记列表，多半是接口改版了")

// NoteMetric 是一条笔记在平台上的真实数据。
//
// 字段用「拿得到就填、拿不到就是 -1」而不是零值：
// 公众号接口不返回评论数，那是「没有这个数」，不是「0 条评论」。
type NoteMetric struct {
	Key         string `json:"key"` // 平台侧 id，能对上就优先用它匹配
	Title       string `json:"title"`
	URL         string `json:"url"`
	Views       int    `json:"views"`
	Likes       int    `json:"likes"`
	Collects    int    `json:"collects"`
	Comments    int    `json:"comments"`
	PublishedAt string `json:"publishedAt"` // yyyy-mm-dd，拿不到留空
}

// FetchMetrics 用已登录的浏览器把这个平台的笔记数据拉一遍。
func (e *Engine) FetchMetrics(platform string) ([]NoteMetric, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	p, ok := Get(platform)
	if !ok {
		return nil, fmt.Errorf("未知平台: %q", platform)
	}
	if p.Kind != KindBrowser || p.MetricsURL == "" || len(p.MetricsAPIs) == 0 {
		return nil, ErrNoMetrics
	}
	if !e.authedLocked(platform) {
		return nil, ErrNotLoggedIn
	}

	ctx, err := e.ensure(platform)
	if err != nil {
		return nil, err
	}

	e.logf("正在打开%s的笔记管理页…", p.Name)
	// 导航到管理页，但不等它渲染完：我们要的是这个页面所在的域和会话，
	// 数据本身靠下面的接口拿。等渲染反而会卡在懒加载的图表上。
	nctx, ncancel := context.WithTimeout(ctx, 30*time.Second)
	defer ncancel()
	if err := chromedp.Run(nctx, chromedp.Navigate(p.MetricsURL)); err != nil {
		return nil, fmt.Errorf("打不开%s的笔记管理页: %w", p.Name, err)
	}

	var lastErr error
	for _, api := range p.MetricsAPIs {
		body, err := fetchInPage(ctx, api)
		if err != nil {
			lastErr = err
			continue
		}
		notes := pickNotes(body)
		if len(notes) == 0 {
			lastErr = ErrMetricsShape
			continue
		}
		e.logf("从%s取到 %d 条笔记的数据", p.Name, len(notes))
		return fillNoteURLs(p.Key, notes), nil
	}
	if lastErr == nil {
		lastErr = ErrMetricsShape
	}
	return nil, lastErr
}

// fillNoteURLs 给没带链接的笔记补一条能打开的地址。
//
// 小红书的取数接口只给 note id，不给链接。没有链接的记录在台账里
// 就是一行光秃秃的标题，用户想回头看看原文得自己去搜——补一条
// 虽然简单，但它是「点得开」和「点不开」的区别。
func fillNoteURLs(platform string, notes []NoteMetric) []NoteMetric {
	patterns := map[string]string{
		"xiaohongshu": "https://www.xiaohongshu.com/explore/%s",
		"douyin":      "https://www.douyin.com/video/%s",
	}
	pat, ok := patterns[platform]
	if !ok {
		return notes
	}
	for i := range notes {
		if strings.TrimSpace(notes[i].URL) == "" && notes[i].Key != "" {
			notes[i].URL = fmt.Sprintf(pat, notes[i].Key)
		}
	}
	return notes
}

// fetchInPage 在页面自己的上下文里发一个 GET。
//
// 为什么不从 Go 直接发 HTTP：平台的接口要带上页面所在域的 cookie，
// 还得有正确的 Referer 和签名头。在页面里 fetch 天然全都带上了，
// 也不用我们自己去解 Chrome 的加密 cookie 库（Windows 上是 DPAPI，
// 解起来又长又容易随版本变）。
func fetchInPage(ctx context.Context, api string) ([]byte, error) {
	// 返回文本而不是对象：ReturnByValue 对复杂对象的序列化有坑，
	// 而且我们本来就要在 Go 里自己解析，多一层 JSON 编解码不亏。
	js := fmt.Sprintf(`(async () => {
		const r = await fetch(%q, {
			method: 'GET',
			credentials: 'include',
			headers: { 'Accept': 'application/json, text/plain, */*' }
		});
		if (!r.ok) return '__HTTP_' + r.status;
		return await r.text();
	})()`, api)

	var (
		val    string
		runErr error
	)
	rctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	runErr = chromedp.Run(rctx, chromedp.ActionFunc(func(ctx context.Context) error {
		res, exc, err := runtime.Evaluate(js).
			WithAwaitPromise(true).
			WithReturnByValue(true).
			Do(ctx)
		if err != nil {
			return err
		}
		// 页面里的 JS 自己抛错时 err 是 nil，只有 exceptionDetails 有内容。
		// 不查这一项的话，网络层失败会被当成「拿到空数据」，接着就报
		// 「接口改版了」——把用户引到完全错误的方向。
		if exc != nil && exc.Text != "" {
			if d := exc.Exception; d != nil && d.Description != "" {
				return fmt.Errorf("页面里执行取数脚本出错: %s", clipRunes(d.Description, 200))
			}
			return fmt.Errorf("页面里执行取数脚本出错: %s", exc.Text)
		}
		if res == nil || res.Value == nil {
			return errors.New("页面没有返回内容")
		}
		return json.Unmarshal(res.Value, &val)
	}))
	if runErr != nil {
		return nil, fmt.Errorf("取数请求失败: %w", runErr)
	}
	if strings.HasPrefix(val, "__HTTP_") {
		code := strings.TrimPrefix(val, "__HTTP_")
		// 401/403 在这里几乎只有一个意思：登录态过期了。
		if code == "401" || code == "403" {
			return nil, ErrNotLoggedIn
		}
		return nil, fmt.Errorf("接口返回 HTTP %s", code)
	}
	return []byte(val), nil
}

// pickNotes 在平台返回的 JSON 里找出笔记数组。
//
// 不做结构体映射：这些接口的字段名换过好几轮（view_count / viewCount /
// views），套一个 struct 就是给自己埋一个「某天全是 0」的雷。
// 这里改成「按已知键找数组，再按候选字段名取值」——平台挪字段不会全崩，
// 只会少某个数，而少了的那个数我们填 -1，界面照样诚实。
func pickNotes(raw []byte) []NoteMetric {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil
	}
	arr := findNoteArray(v, 0)
	out := make([]NoteMetric, 0, len(arr))
	for _, m := range arr {
		// 平台常把计数塞在子对象里（抖音就是 statistics），
		// 顶层找不到值时要再看这一层——否则整条笔记的数据全是 -1，
		// 用户会以为「自动取数根本没用」。
		layers := []map[string]any{m}
		for _, k := range []string{"statistics", "stats", "stats_v2", "count_info"} {
			if child, ok := m[k].(map[string]any); ok {
				layers = append(layers, child)
			}
		}
		nm := NoteMetric{
			Key:         str(m, "id", "note_id", "noteId", "noteid", "item_id", "aweme_id", "msgid", "msg_id"),
			Title:       str(m, "title", "display_title", "desc", "note_title"),
			URL:         str(m, "url", "share_url", "note_url", "link"),
			Views:       countLayers(layers, "view_count", "viewCount", "views", "read_count", "int_page_read_count", "play_count", "playCount"),
			Likes:       countLayers(layers, "likes", "like_count", "liked_count", "likeCount", "digg_count", "share_count"),
			Collects:    countLayers(layers, "collected_count", "collect_count", "collects", "fav_count", "add_to_fav_count"),
			Comments:    countLayers(layers, "comments_count", "comment_count", "comments", "commentCount"),
			PublishedAt: date(m, "time", "create_time", "createTime", "publish_time", "ref_date", "posted_at"),
		}
		// 一条笔记至少得有个标题或 id 才认，否则是数组里混进来的统计对象。
		if nm.Title == "" && nm.Key == "" {
			continue
		}
		out = append(out, nm)
	}
	return out
}

// findNoteArray 广度优先找「像笔记列表」的数组。
//
// 先按已知键取，取不到再往下钻一层。限制深度是为了不在一个几 MB 的
// 配置对象里乱翻，也避免某些接口里的推荐位数组被当成笔记列表。
func findNoteArray(v any, depth int) []map[string]any {
	if depth > 5 {
		return nil
	}
	switch t := v.(type) {
	case map[string]any:
		for _, k := range []string{"notes", "note_list", "noteList", "items", "list", "works", "aweme_list", "data"} {
			child, ok := t[k]
			if !ok {
				continue
			}
			if arr, ok := child.([]any); ok {
				if out := mapsOf(arr); len(out) > 0 {
					return out
				}
			}
		}
		for _, child := range t {
			if out := findNoteArray(child, depth+1); len(out) > 0 {
				return out
			}
		}
	case []any:
		for _, child := range t {
			if out := findNoteArray(child, depth+1); len(out) > 0 {
				return out
			}
		}
	}
	return nil
}

// mapsOf 只保留元素是对象的项，并要求它们看起来像笔记（有标题或 id）。
func mapsOf(arr []any) []map[string]any {
	out := make([]map[string]any, 0, len(arr))
	for _, e := range arr {
		m, ok := e.(map[string]any)
		if !ok {
			continue
		}
		if str(m, "title", "display_title", "desc", "note_title", "id", "note_id") == "" {
			continue
		}
		out = append(out, m)
	}
	return out
}

// str 取第一个非空字符串字段。
func str(m map[string]any, keys ...string) string {
	for _, k := range keys {
		v, ok := m[k]
		if !ok || v == nil {
			continue
		}
		s := strings.TrimSpace(fmt.Sprint(v))
		// 有些接口把 id 存成数字，fmt 会打出科学计数法的浮点。
		if s != "" && s != "<nil>" {
			return s
		}
	}
	return ""
}

// count 取第一个能解析的数字字段，解析不出来就是 -1（未取到）。
func count(m map[string]any, keys ...string) int {
	for _, k := range keys {
		v, ok := m[k]
		if !ok || v == nil {
			continue
		}
		if n, ok := parseCount(v); ok {
			return n
		}
	}
	return -1
}

// countLayers 按「外层优先」的层次找值：先在第一层把所有候选键试完，
// 再下到第二层。
//
// 顺序不能反过来——每层都试一遍候选键、取第一个命中的层，
// 才是「这条笔记自己的数」优先于「它下面某个子对象里的同名数」。
func countLayers(layers []map[string]any, keys ...string) int {
	for _, m := range layers {
		if n := count(m, keys...); n >= 0 {
			return n
		}
	}
	return -1
}

// parseCount 把各种形状的计数解析成整数。
//
// 网页 DOM 里是「1.2万」「10w+」，接口里是 1234 或 "1234"，
// 两种都要能吃下——否则换个抓法就要改一遍解析。
func parseCount(v any) (int, bool) {
	switch t := v.(type) {
	case float64:
		if math.IsNaN(t) || math.IsInf(t, 0) {
			return 0, false
		}
		return int(t), true
	case int:
		return t, true
	case int64:
		return int(t), true
	case json.Number:
		n, err := t.Int64()
		if err != nil {
			// 有的接口用浮点（1.0），退一步按 float 解。
			f, ferr := t.Float64()
			if ferr != nil {
				return 0, false
			}
			return int(f), true
		}
		return int(n), true
	case string:
		return parseCountText(t)
	}
	return 0, false
}

var countRe = regexp.MustCompile(`(?i)^([0-9]+(?:\.[0-9]+)?)\s*([万亿kw]?)\s*\+?$`)

// parseCountText 解析「1.2万」这类中文计数文案。
func parseCountText(s string) (int, bool) {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, ",", "")
	if s == "" {
		return 0, false
	}
	m := countRe.FindStringSubmatch(s)
	if m == nil {
		return 0, false
	}
	f, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, false
	}
	switch strings.ToLower(m[2]) {
	case "万", "w":
		f *= 10000
	case "亿":
		f *= 100000000
	case "k":
		f *= 1000
	}
	// 「1.2万」这种是平台的展示精度，取整到万位更诚实：
	// 报 12000 会让人以为精确到个位，而它其实只是「1.2 万左右」。
	return int(f + 0.5), true
}

// date 把平台的时间戳或日期字段统一成 yyyy-mm-dd。
func date(m map[string]any, keys ...string) string {
	for _, k := range keys {
		v, ok := m[k]
		if !ok || v == nil {
			continue
		}
		if s := toDate(v); s != "" {
			return s
		}
	}
	return ""
}

func toDate(v any) string {
	switch t := v.(type) {
	case string:
		s := strings.TrimSpace(t)
		if len(s) >= 10 && s[4] == '-' && s[7] == '-' {
			return s[:10]
		}
		// 有些接口给的是秒级时间戳的字符串。
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return epochDate(n)
		}
		return ""
	case float64:
		return epochDate(int64(t))
	case int64:
		return epochDate(t)
	case json.Number:
		if n, err := t.Int64(); err == nil {
			return epochDate(n)
		}
	}
	return ""
}

// epochDate 区分秒和毫秒：接口两种都有，差 1000 倍会算到 1970 年去。
func epochDate(n int64) string {
	if n <= 0 {
		return ""
	}
	if n > 1e12 {
		n /= 1000
	}
	return time.Unix(n, 0).Format("2006-01-02")
}

// ArticleStats 用公众号官方接口取图文数据。
//
// 这是唯一有官方数据接口的平台，比抓网页稳得多——所以公众号不排进
// 「自动取数」的黑名单，走这条路。
//
// 返回的 Comments 恒为 -1：公众号的数据接口不返回评论数，
// 评论要另调 comment/list 且需要每篇文章的 msg_data_id，代价和收益不成比例。
func (w *WeChat) ArticleStats(ctx context.Context, begin, end string) ([]NoteMetric, error) {
	if !w.Configured() {
		return nil, errors.New("公众号还没配 AppID / AppSecret")
	}
	token, err := w.Token(ctx)
	if err != nil {
		return nil, err
	}
	payload, _ := json.Marshal(map[string]string{
		"begin_date": begin,
		"end_date":   end,
	})
	var out struct {
		List []map[string]any `json:"list"`
	}
	url := w.base() + "/datacube/getarticletotaldetail?access_token=" + token
	if err := w.postJSON(ctx, url, payload, &out); err != nil {
		return nil, err
	}
	metrics := make([]NoteMetric, 0, len(out.List))
	for _, m := range out.List {
		mm := NoteMetric{
			Key:         str(m, "msgid", "msg_id"),
			Title:       str(m, "title"),
			URL:         str(m, "content_url", "url"),
			Views:       count(m, "int_page_read_count", "read_count"),
			Likes:       count(m, "share_count", "like_count"),
			Collects:    count(m, "add_to_fav_count", "collect_count"),
			Comments:    -1, // 见函数注释
			PublishedAt: toDate(str(m, "ref_date")),
		}
		if mm.Key == "" && mm.Title == "" {
			continue
		}
		metrics = append(metrics, mm)
	}
	return metrics, nil
}
