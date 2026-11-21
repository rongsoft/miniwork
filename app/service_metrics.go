package main

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"

	"rongsoft/miniwork/app/internal/publish"
	"rongsoft/miniwork/app/internal/store"
)

// 这一块解决一个很具体的问题：数据复盘页原来要求用户自己抄四个数。
//
// 手抄的失败方式不是"抄错"，是"不抄"。用户发完笔记就去忙别的，
// 三天后想复盘，要么懒得翻回去抄，要么记不清哪条是哪条。
// 结果就是复盘页常年空着，而这一页正是整个工具里唯一能用真实结果
// 校准直觉的地方——空着的复盘页等于没有复盘。
//
// 现在有内置浏览器和登录态，这些数就在平台的笔记管理页里。
// 但自动取数不是"更省事的填法"，它的边界必须说清楚：
//   - 只覆盖同平台、能对上的记录；
//   - 对不上的不猜、不塞给最像的那条（错配比没数据更坏）；
//   - 平台没返回的项保持原值，不当成 0。

// SyncResult 是一次同步的结果，界面直接照着显示。
type SyncResult struct {
	Platform     string `json:"platform"`
	PlatformName string `json:"platformName"`
	Fetched      int    `json:"fetched"`   // 平台上取到几条
	Matched      int    `json:"matched"`   // 对上并写回几条
	Unmatched    int    `json:"unmatched"` // 取到了但在台账里找不到对应记录
	SyncedAt     string `json:"syncedAt"`  // 15:04
}

// MetricsSupport 告诉界面「哪些平台能自动取数」。
//
// 这个判断放在后端而不是前端写死：平台能力是事实表的一部分，
// 多一个平台支持自动取数时，只该改 platform.go 一张表，
// 而不是同时去前端再补一遍分支。
type MetricsSupport struct {
	Platform     string `json:"platform"`
	PlatformName string `json:"platformName"`
	Auto         bool   `json:"auto"`   // 能不能自动取
	Reason       string `json:"reason"` // 不能的话，为什么
	Count        int    `json:"count"`  // 台账里这个平台记了几条
}

// SyncNoteMetrics 从一个平台抓数据并写回台账。
//
// 公众号走官方接口，其余走浏览器。两条路都不需要用户做任何事——
// 除了公众号要先在设置里配好 AppID/AppSecret，小红书要登录过一次。
func (s *TopicService) SyncNoteMetrics(platform string) (SyncResult, error) {
	p, ok := publish.Get(platform)
	if !ok {
		return SyncResult{}, fmt.Errorf("未知平台: %q", platform)
	}
	res := SyncResult{Platform: p.Key, PlatformName: p.Name}

	var (
		metrics []publish.NoteMetric
		err     error
	)
	if p.Kind == publish.KindAPI {
		metrics, err = s.wechatMetrics()
	} else {
		metrics, err = s.browserMetrics(platform)
	}
	if err != nil {
		return res, err
	}
	res.Fetched = len(metrics)
	if len(metrics) == 0 {
		// 一条都没有不算成功：要么接口改版，要么这个号真的还没发过东西。
		// 两种情况界面都要给句话，所以这里返回一个错误，由界面决定怎么说。
		return res, fmt.Errorf("平台上没取到任何笔记")
	}

	patches, unmatched, err := s.matchMetrics(platform, metrics)
	if err != nil {
		return res, err
	}
	n, err := s.st.ApplyMetrics(patches)
	if err != nil {
		return res, fmt.Errorf("写回台账失败: %w", err)
	}
	res.Matched = n
	res.Unmatched = unmatched
	res.SyncedAt = time.Now().Format("15:04")
	return res, nil
}

// browserMetrics 走浏览器那条路。
func (s *TopicService) browserMetrics(platform string) ([]publish.NoteMetric, error) {
	ps, err := s.publisher()
	if err != nil {
		return nil, err
	}
	return ps.eng.FetchMetrics(platform)
}

// wechatMetrics 走公众号官方接口。
//
// 取最近 30 天：数据接口的跨度上限就在这里，而公众号可能隔几天才发一次，
// 窗口再短就会漏掉那些"一周一篇"的号。
func (s *TopicService) wechatMetrics() ([]publish.NoteMetric, error) {
	wx := s.wechat()
	if !wx.Configured() {
		return nil, fmt.Errorf("公众号还没配 AppID / AppSecret，先去设置里填")
	}
	now := time.Now()
	begin := now.AddDate(0, 0, -29).Format(store.DateLayout)
	end := now.Format(store.DateLayout)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return wx.ArticleStats(ctx, begin, end)
}

// matchMetrics 把平台数据对到台账记录上，产出要写回的补丁。
//
// 匹配顺序是「链接 → 标题原文 → 标题去噪」。
// 不用"最像的那条"这种模糊匹配：一条数据挂到别人名下，用户看到的
// 是一条根本没发过的笔记有播放量，这比没有数据难查得多。
func (s *TopicService) matchMetrics(platform string, metrics []publish.NoteMetric) ([]store.MetricPatch, int, error) {
	recs, err := s.st.ListPublish(300)
	if err != nil {
		return nil, 0, err
	}
	// 只考虑这个平台的记录：拿小红书的数据去填抖音那条，
	// 不管标题多像都是错的。
	byURL := map[string]string{}   // 链接 → 记录 id
	byKey := map[string]string{}   // 链接里的笔记 id → 记录 id
	byTitle := map[string]string{} // 归一化标题 → 记录 id
	for _, r := range recs {
		if r.Platform != platform {
			continue
		}
		if u := strings.TrimSpace(r.URL); u != "" {
			byURL[u] = r.ID
			if k := urlNoteKey(u); k != "" {
				byKey[k] = r.ID
			}
		}
		if t := normTitle(s.topicTitle(r.TopicID)); t != "" {
			byTitle[t] = r.ID
		}
	}

	patches := make([]store.MetricPatch, 0, len(metrics))
	used := map[string]bool{}
	unmatched := 0
	for _, m := range metrics {
		id := ""
		if u := strings.TrimSpace(m.URL); u != "" {
			if v, ok := byURL[u]; ok {
				id = v
			}
		}
		// 链接全等很少见：用户粘的是带 xsec_token 的分享链接，
		// 平台返回的是不带参数的 explore/<id>。比 id 才靠谱。
		if id == "" && m.Key != "" {
			if v, ok := byKey[m.Key]; ok {
				id = v
			}
		}
		if id == "" {
			if v, ok := byTitle[normTitle(m.Title)]; ok {
				id = v
			}
		}
		if id == "" {
			unmatched++
			continue
		}
		// 同一个平台两次返回同一条（分页重叠）时只用第一次，
		// 后面的数可能更旧。
		if used[id] {
			continue
		}
		used[id] = true
		patches = append(patches, store.MetricPatch{
			ID: id, Views: m.Views, Likes: m.Likes,
			Collects: m.Collects, Comments: m.Comments, URL: m.URL,
		})
	}
	return patches, unmatched, nil
}

// topicTitle 取选题标题；取不到返回空串（匹配时跳过，不拿空串去对）。
func (s *TopicService) topicTitle(topicID string) string {
	if strings.TrimSpace(topicID) == "" {
		return ""
	}
	t, err := s.st.TopicByID(topicID)
	if err != nil {
		return ""
	}
	return t.Title
}

// urlNoteKey 从一条分享链接里取出平台侧的笔记 id。
//
// 用户粘的是带一堆参数的分享链接（xsec_token、xsec_source 之类），
// 平台接口返回的却是不带参数的 explore/<id>。直接比整条字符串永远不相等，
// 于是每条都得退回标题匹配——而标题可能改过、可能带了话题标签。
// 抽出 id 来比，命中率立刻不一样。
func urlNoteKey(u string) string {
	u = strings.TrimSpace(u)
	if u == "" {
		return ""
	}
	// 砍掉查询串和锚点，只看路径。
	if i := strings.IndexAny(u, "?#"); i >= 0 {
		u = u[:i]
	}
	u = strings.TrimRight(u, "/")
	i := strings.LastIndex(u, "/")
	if i < 0 || i == len(u)-1 {
		return ""
	}
	seg := u[i+1:]
	// 太短的（比如路径词 item、note）不算 id，免得把两条不同的笔记
	// 都归到同一个 key 上。
	if len(seg) < 6 {
		return ""
	}
	for _, r := range seg {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return ""
		}
	}
	return seg
}

// hashTagRe 匹配话题标签：# 后面连续的非空白字符。
//
// 全角的＃也认：平台展示层两种都用过，而用户不会去分清自己打的是哪个。
var hashTagRe = regexp.MustCompile(`[#＃][^\s#＃]+`)

// normTitle 归一化标题用于比较。
//
// 平台上的标题和台账里的标题经常只差标点、空格、话题标签：
// 台账里是「一个人也能跑通的自媒体工作台」，平台上可能带了个「#效率工具」。
// 先砍掉话题标签，再只留字母数字和汉字，两边的差异就消失了——但顺序和
// 内容必须一致，不做相似度，免得把两条不同的笔记混淆。
func normTitle(s string) string {
	s = hashTagRe.ReplaceAllString(s, " ")
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

// MetricsPlatforms 列出台账里出现过的平台，以及各自能不能自动取数。
//
// 名字不带 Support 是因为要和上面的类型区分开：一个叫 MetricsSupport
// 的方法返回 []MetricsSupport，读代码的人会以为它是构造函数。
func (s *TopicService) MetricsPlatforms() ([]MetricsSupport, error) {
	recs, err := s.st.ListPublish(300)
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for _, r := range recs {
		counts[r.Platform]++
	}

	out := make([]MetricsSupport, 0, len(publish.All()))
	for _, p := range publish.All() {
		n := counts[p.Key]
		if n == 0 {
			// 没发过的平台不占位置：一个从没用过的按钮只会让人问"这是干嘛的"。
			continue
		}
		v := MetricsSupport{
			Platform: p.Key, PlatformName: p.Name, Count: n, Auto: true,
		}
		switch {
		case p.Kind == publish.KindAPI:
			if !s.wechat().Configured() {
				v.Auto = false
				v.Reason = "要先在设置里配好公众号的 AppID / AppSecret"
			}
		case p.MetricsURL == "":
			v.Auto = false
			v.Reason = "这个平台没有公开的数据页"
		default:
			if ps, err := s.publisher(); err == nil && !ps.eng.Authed(p.Key) {
				v.Auto = false
				v.Reason = "还没登录" + p.Name + "，去发布台扫码登录一次就行"
			}
		}
		out = append(out, v)
	}
	return out, nil
}
