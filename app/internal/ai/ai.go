// Package ai 负责「让模型判断这条内容适不适合这个作者」。
//
// 它只管三件事：拼提示词、发请求、解析结果。不碰数据库、不碰状态机。
// 缓存和降级都在 pipeline 层做，因为那需要知道「这一批算不算同一次任务」。
package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

var (
	// ErrQuotaExceeded 表示模型额度用完，调用方应退回规则打分。
	ErrQuotaExceeded = errors.New("模型额度用尽")
	// ErrUnauthorized 表示密钥无效，重试没有意义。
	ErrUnauthorized = errors.New("模型密钥无效")
	// ErrNotConfigured 表示没配模型，等同于关闭。
	ErrNotConfigured = errors.New("模型未配置")
)

// Brief 是喂给模型的「你是谁」。字段少而具体，是因为模型真正需要的只有这些。
type Brief struct {
	Niche       string
	Positioning string
	Audience    string
	Tone        string
	Avoid       string
	Picked      []string // 最近收下的标题
	Dropped     []string // 最近丢掉的标题
}

// Item 是一条待判断的候选内容。
type Item struct {
	ID       string
	Platform string
	Title    string
	Heat     int64
	AgeLabel string
}

// Result 是模型对一条内容的判断。
type Result struct {
	Fit    int    // 0-100，与作者的契合度
	Reason string // 一句话，说为什么值得/不值得写
}

// Client 是模型调用的抽象。relay 与 byok 都实现它。
type Client interface {
	Name() string
	// Complete 发一次对话请求，返回助手回复的正文。
	Complete(ctx context.Context, system, user string) (string, error)
}

// Creative 是可选的「写稿通道」。
//
// 做成可选接口而不是往 Client 里加参数：打分只需要 Complete，
// 测试里的假 client 也只实现它。往接口里加方法，所有实现都得跟着改，
// 而这些改动对打分毫无意义。
type Creative interface {
	CompleteCreative(ctx context.Context, system, user string) (string, error)
}

// Write 是写稿的唯一调用入口。
// 有创意通道就走它（高温度、长超时），没有就退回普通通道，
// 让功能在「配了个不支持 scene 的服务端」时仍然能用，只是稿子保守一点。
func Write(ctx context.Context, c Client, system, user string) (string, error) {
	if cr, ok := c.(Creative); ok {
		return cr.CompleteCreative(ctx, system, user)
	}
	return c.Complete(ctx, system, user)
}

// 记忆段最多带这么多条。带太多会挤掉候选内容，也更容易让模型
// 把「历史偏好」过度推广成「只写这一类」。
const memoryCap = 8

// BuildPrompt 拼出 system 与 user 两段提示词。
//
// 这段文案是产品的核心资产之一，改动要当成改产品而不是改代码：
//   - 明确「跑题的爆款必须低分」，否则模型会被热度带偏
//   - 明确「禁止复述数据」，否则理由会退化成「18 万热度，2 小时前」
//   - 明确要求区分度，否则模型会给所有条目 80 分，等于没打分
func BuildPrompt(b Brief, items []Item) (system, user string) {
	system = `你是一个自媒体选题顾问。你的任务不是夸每条内容好，而是判断「这条适不适合这个作者写」。

判断依据，按重要性排序：
1. 与他的定位和目标读者是否真相关。跑题的爆款也必须给低分。
2. 是否有可写的差异化角度。人人都在写的通稿角度，分要低。
3. 是否撞上他明确说过不碰的话题。撞上直接给 0-20 分。

打分要有区分度：真正值得他写的 75-90，可写可不写 50-65，跑题 20-40。
不要因为热度高就给高分——热度只说明别人写过，不代表他该写。

理由要求：一句话，不超过 28 个字，说「为什么值得他写」或「为什么不值得」。
禁止复述热度、时间、平台这些列表上已经看得见的数据。
禁止空话，「很有价值」「值得一看」这类不算理由。

只输出 JSON 数组，不要解释，不要 markdown 代码块。格式：
[{"id":"原样返回的 id","fit":0到100的整数,"reason":"一句话"}]`

	var sb strings.Builder
	sb.WriteString("作者信息：\n")
	writeIf := func(label, v string) {
		if v = strings.TrimSpace(v); v != "" {
			sb.WriteString("- " + label + "：" + v + "\n")
		}
	}
	writeIf("赛道", b.Niche)
	writeIf("定位", b.Positioning)
	writeIf("目标读者", b.Audience)
	writeIf("语气风格", b.Tone)
	writeIf("不碰的话题", b.Avoid)

	if strings.TrimSpace(b.Positioning) == "" && strings.TrimSpace(b.Audience) == "" {
		sb.WriteString("- （作者还没填写人设，只按赛道和标题判断，不要假设他的身份）\n")
	}

	if p, d := capList(b.Picked, memoryCap), capList(b.Dropped, memoryCap); len(p) > 0 || len(d) > 0 {
		sb.WriteString("\n他过去的选择：\n")
		if len(p) > 0 {
			sb.WriteString("收下过的（说明他偏这类）：\n")
			for _, t := range p {
				sb.WriteString("- " + t + "\n")
			}
		}
		if len(d) > 0 {
			sb.WriteString("丢掉过的（说明这类别再推）：\n")
			for _, t := range d {
				sb.WriteString("- " + t + "\n")
			}
		}
	}

	sb.WriteString("\n候选内容（共 " + strconv.Itoa(len(items)) + " 条）：\n")
	for _, it := range items {
		fmt.Fprintf(&sb, "id=%s | %s | %s互动 | %s | %s\n",
			it.ID, platformName(it.Platform), heatLabel(it.Heat), it.AgeLabel, it.Title)
	}
	return system, sb.String()
}

func capList(s []string, n int) []string {
	out := make([]string, 0, n)
	for _, v := range s {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
		if len(out) == n {
			break
		}
	}
	return out
}

// Parse 解析模型回复。容忍模型在 JSON 前后加话、加代码块。
func Parse(content string) (map[string]Result, error) {
	arr, err := extractArray(content)
	if err != nil {
		return nil, err
	}

	var raws []struct {
		ID     any `json:"id"`
		Fit    any `json:"fit"`
		Reason any `json:"reason"`
	}
	if err := json.Unmarshal(arr, &raws); err != nil {
		return nil, fmt.Errorf("模型返回的不是 JSON 数组: %w", err)
	}

	out := make(map[string]Result, len(raws))
	for _, r := range raws {
		id := strings.TrimSpace(toStr(r.ID))
		if id == "" {
			continue
		}
		out[id] = Result{
			Fit:    clampFit(toInt(r.Fit)),
			Reason: clip(toStr(r.Reason), 40),
		}
	}
	if len(out) == 0 {
		return nil, errors.New("模型返回了空结果")
	}
	return out, nil
}

// extractArray 从回复里抠出 JSON 数组本体。
// 模型几乎总会加一句「以下是结果：」或者包一层 ```json。
func extractArray(s string) ([]byte, error) {
	start := strings.Index(s, "[")
	end := strings.LastIndex(s, "]")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("模型回复里找不到 JSON 数组: %s", clip(s, 80))
	}
	return []byte(s[start : end+1]), nil
}

func clampFit(n int) int {
	if n < 0 {
		return 0
	}
	if n > 100 {
		return 100
	}
	return n
}

func toInt(v any) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case string:
		// 模型偶尔给 "82分" 这种写法。
		var b strings.Builder
		for _, r := range t {
			if r >= '0' && r <= '9' {
				b.WriteRune(r)
			} else if b.Len() > 0 {
				break
			}
		}
		if n, err := strconv.Atoi(b.String()); err == nil {
			return n
		}
	}
	return 0
}

func toStr(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		// 纯数字 id 会被解成 float，去掉无意义的小数点。
		return strconv.FormatFloat(t, 'f', -1, 64)
	case nil:
		return ""
	default:
		return fmt.Sprint(t)
	}
}

// clip 按字符截断，避免把多字节字符切成半个。
func clip(s string, max int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}

func platformName(p string) string {
	switch strings.ToLower(p) {
	case "xiaohongshu", "xhs":
		return "小红书"
	case "douyin":
		return "抖音"
	}
	return p
}

func heatLabel(n int64) string {
	switch {
	case n >= 10000:
		return fmt.Sprintf("%.1f万", float64(n)/10000)
	case n > 0:
		return strconv.FormatInt(n, 10)
	}
	return "未知"
}
