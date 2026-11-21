// Package source 是外部数据源的唯一入口。
//
// 上层只认这里的 Hotspot 和 Source，不认易撰、不认 HTTP、不认 WebView。
// 换供应商时只加一个实现文件。
package source

import (
	"context"
	"errors"
	"time"
)

// ErrNotConfigured 表示数据源尚未配好凭证（例如易撰 API Key 还没拿到）。
var ErrNotConfigured = errors.New("数据源未配置")

// ErrQuotaExceeded 表示数据源配额用尽，调用方应降级到缓存。
var ErrQuotaExceeded = errors.New("数据源配额用尽")

// Hotspot 是抓回来的一条平台内容，字段与存储层 hotspot 表一一对应。
type Hotspot struct {
	ID          string // 平台内唯一 id，用于去重
	Platform    string // xiaohongshu / douyin / xhs ...
	Title       string
	Author      string
	URL         string
	Heat        int64     // 互动量：点赞+收藏+评论的近似值
	Tags        []string  // 平台话题标签
	PublishedAt time.Time // 平台发布时间，用于时效打分
	FetchedAt   time.Time // 抓取时间，用于判断数据是否陈旧
}

// Query 是一次取数的条件。
//
// Keyword 与 Niche 的关系有个明确的优先级：**有关键词时，赛道不再是筛选条件**。
// 用户输入「裁员」是想看所有和裁员有关的东西，而不是「职场赛道里的裁员」——
// 副业赛道里「公司裁员后我开始做副业」同样是他要的。
// 所以关键词搜索是跨赛道的，Niche 退化成排序时的偏好，不是门槛。
type Query struct {
	Niche   string
	Keyword string    // 用户主动输入；为空表示按赛道常规抓取
	Since   time.Time // 零值由实现决定回溯窗口
}

// Source 是数据源的抽象。三个实现：mock / remote / webview。
type Source interface {
	// Name 用于日志与诊断包，不参与业务判断。
	Name() string
	// Fetch 按条件取候选内容。
	Fetch(ctx context.Context, q Query) ([]Hotspot, error)
}
