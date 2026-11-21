package source

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Remote 连的是我们自己的服务端，不是易撰。
//
// 为什么绕一层：易撰的会员配额是按账号给的（每天 100 次数据搜索、100 次榜单），
// 让每个用户自己买会员比软件本身还贵，共用一把 Key 又会早高峰打爆。
// 所以服务端每小时集中拉一轮、入库、按赛道分发，客户端只读缓存副本。
//
// 契约（服务端实现见 products/miniwork/api-contract.md）：
//
//	GET {base}/data/hotspots?niche=职场&limit=60[&since=2026-11-14T00:00:00Z]
//	Authorization: Bearer <token>
//
//	200 {"stale":false,"generatedAt":"...","items":[
//	      {"id":"xhs_1","platform":"xiaohongshu","title":"...","author":"...",
//	       "url":"...","heat":12345,"tags":["a"],"publishedAt":"..."}]}
//	401 → ErrNotConfigured     （地址对、Key 错）
//	402/429 → ErrQuotaExceeded （服务端自身配额用完）
//
// items 也允许直接是顶层数组，方便服务端临时用 nginx 静态 json 顶一下。
type Remote struct {
	base  string
	token string
	http  *http.Client
}

// NewRemote 构造。base 为空时调用方应改选 mock，这里不做兜底判断，
// 以免 UI 上「示例数据」和「线上数据」分不清。
func NewRemote(base, token string) *Remote {
	return &Remote{
		base:  strings.TrimRight(base, "/"),
		token: token,
		// 8 秒是权衡：弱网下够用，又不至于让首启动卡过 90 秒生死线。
		http: &http.Client{Timeout: 8 * time.Second},
	}
}

func (r *Remote) Name() string { return "remote" }

// Fetch 按赛道取候选内容。返回的 Hotspot 已经带 FetchedAt，可直接落库。
func (r *Remote) Fetch(ctx context.Context, qu Query) ([]Hotspot, error) {
	niche := strings.TrimSpace(qu.Niche)
	keyword := strings.TrimSpace(qu.Keyword)
	// 关键词搜索可以不指定赛道（跨赛道找），但两者都空就没法查了。
	if niche == "" && keyword == "" {
		return nil, errors.New("缺少赛道或关键词参数")
	}
	if r.base == "" {
		return nil, fmt.Errorf("%w: 未填写服务端地址", ErrNotConfigured)
	}

	u, err := url.Parse(r.base + "/data/hotspots")
	if err != nil {
		return nil, fmt.Errorf("%w: 服务端地址不合法: %v", ErrNotConfigured, err)
	}
	q := u.Query()
	if niche != "" {
		q.Set("niche", niche)
	}
	if keyword != "" {
		q.Set("keyword", keyword)
	}
	q.Set("limit", "60")
	if !qu.Since.IsZero() {
		q.Set("since", qu.Since.UTC().Format(time.RFC3339))
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Miniwork/0.1")
	if r.token != "" {
		req.Header.Set("Authorization", "Bearer "+r.token)
	}

	resp, err := r.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("连不上数据服务: %w", err)
	}
	defer resp.Body.Close()

	// 限长读取：上游返回一个超大 body 时不该把桌面端内存吃掉。
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusOK:
		// 继续往下解析
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, fmt.Errorf("%w: 密钥无效或已过期", ErrNotConfigured)
	case http.StatusPaymentRequired, http.StatusTooManyRequests:
		return nil, fmt.Errorf("%w", ErrQuotaExceeded)
	default:
		return nil, fmt.Errorf("数据服务返回 %d: %s", resp.StatusCode, snippet(body))
	}

	return parseItems(body, time.Now())
}

// wireItem 是线上 json 的形态。字段名两种都认：服务端用 Java + Jackson
// 时很容易写成 snake_case，改一次字段名不该让客户端白屏。
type wireItem struct {
	ID               string   `json:"id"`
	Platform         string   `json:"platform"`
	Title            string   `json:"title"`
	Author           string   `json:"author"`
	URL              string   `json:"url"`
	Heat             int64    `json:"heat"`
	Tags             []string `json:"tags"`
	PublishedAt      string   `json:"publishedAt"`
	PublishedAtSnake string   `json:"published_at"`
}

type wireBody struct {
	Stale bool       `json:"stale"`
	Items []wireItem `json:"items"`
}

func parseItems(body []byte, now time.Time) ([]Hotspot, error) {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return nil, errors.New("数据服务返回空响应")
	}

	var items []wireItem
	if strings.HasPrefix(trimmed, "[") {
		if err := json.Unmarshal(body, &items); err != nil {
			return nil, fmt.Errorf("解析数据失败: %w", err)
		}
	} else {
		var wb wireBody
		if err := json.Unmarshal(body, &wb); err != nil {
			return nil, fmt.Errorf("解析数据失败: %w", err)
		}
		items = wb.Items
	}

	seen := make(map[string]bool, len(items))
	out := make([]Hotspot, 0, len(items))
	for _, it := range items {
		id := strings.TrimSpace(it.ID)
		title := strings.TrimSpace(it.Title)
		// 没有 id 就没法幂等去重，没有标题就没法打分——两者缺一都直接丢。
		if id == "" || title == "" || seen[id] {
			continue
		}
		seen[id] = true

		pub := it.PublishedAt
		if pub == "" {
			pub = it.PublishedAtSnake
		}
		out = append(out, Hotspot{
			ID:          id,
			Platform:    defaultPlatform(it.Platform),
			Title:       title,
			Author:      it.Author,
			URL:         it.URL,
			Heat:        it.Heat,
			Tags:        it.Tags,
			PublishedAt: parseTime(pub, now),
			FetchedAt:   now,
		})
	}
	return out, nil
}

func defaultPlatform(p string) string {
	p = strings.ToLower(strings.TrimSpace(p))
	if p == "" {
		return "xiaohongshu"
	}
	return p
}

// parseTime 容忍三种写法：RFC3339、带空格的日期时间、纯日期。
// 解析不出来时按「刚刚」处理，宁可时效分给高一点，也不要出现 0001 年。
func parseTime(s string, now time.Time) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return now
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02T15:04:05", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t
		}
	}
	return now
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	const max = 120
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}
