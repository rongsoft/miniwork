package ai

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Digest 是一条素材的提炼结果：一段摘要 + 几条值得学的点。
//
// 为什么不复用 Draft：稿子是「我要写的」，摘要是「别人怎么写的」。
// 两者字段名很像（都有 title/body），但语义完全不同——把它们合并成一个
// 结构，以后改稿子的字段时会顺手把素材那块改坏，反之亦然。
type Digest struct {
	Summary string
	Points  []string
}

// DigestSystem 是提炼素材的 system prompt。
//
// 定成常量而不是拼在调用处：这段文字决定了「值得学习的点」长什么样，
// 用户会拿它当判断标准。散在业务代码里，改一次措辞谁都不知道。
const DigestSystem = `你是自媒体内容拆解助手。用户会给你一篇别人写的笔记正文。

请你做两件事：
1. 用 60-90 字概括这篇笔记讲了什么、结论是什么。不要复述开头，直接给干货。
2. 提炼 3 条「值得学习的点」——写的人做对了什么。要具体到可操作，
   例如「标题用数字+反差」、「开头先给结论再讲过程」、「每个观点都配了自己的经历」。

只输出 JSON，不要任何解释和代码块标记：
{"summary":"...","points":["...","...","..."]}`

// DigestUser 把素材拼成一次提问。
//
// 正文截断到 4000 字：再长的内容喂进去会把上下文窗口吃满，
// 而摘要本来只需要主体结构，前 4000 字足够看出写法。
func DigestUser(title, body string) string {
	var b strings.Builder
	if t := strings.TrimSpace(title); t != "" {
		b.WriteString("标题：")
		b.WriteString(t)
		b.WriteString("\n\n")
	}
	body = strings.TrimSpace(body)
	if len([]rune(body)) > 4000 {
		body = string([]rune(body)[:4000]) + "\n…（后文省略）"
	}
	b.WriteString(body)
	return b.String()
}

// ParseDigest 解析提炼结果。
//
// 容忍 points 给成字符串数组以外的写法：模型偶尔会返回一整个字符串
// （用换行分隔三条），那种情况按行拆开，比直接报错让用户白等一次好。
func ParseDigest(content string) (Digest, error) {
	raw, err := extractObject(content)
	if err != nil {
		return Digest{}, err
	}
	var r struct {
		Summary any `json:"summary"`
		Points  any `json:"points"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return Digest{}, fmt.Errorf("提炼结果不是合法 JSON: %w", err)
	}

	d := Digest{Summary: strings.TrimSpace(toStr(r.Summary))}
	switch v := r.Points.(type) {
	case []any:
		for _, p := range v {
			if s := strings.TrimSpace(toStr(p)); s != "" {
				d.Points = append(d.Points, s)
			}
		}
	case string:
		for _, line := range strings.Split(v, "\n") {
			line = strings.TrimSpace(strings.TrimLeft(line, "-·•0123456789.、 "))
			if line != "" {
				d.Points = append(d.Points, line)
			}
		}
	}

	// 摘要为空等于这次提炼没做成：界面上会显示成「提炼完成但什么都没有」，
	// 比报错更难查。宁可返回错误，让调用方把状态留在「待提炼」。
	if d.Summary == "" {
		return Digest{}, fmt.Errorf("提炼结果里没有摘要")
	}
	// 要点最多留 4 条：模型有时会把一整段观点拆成七八条，
	// 那种密度在卡片上没法看，也不符合「值得学的点是少数」这个事实。
	if len(d.Points) > 4 {
		d.Points = d.Points[:4]
	}
	return d, nil
}
