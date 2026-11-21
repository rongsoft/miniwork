// Package draft 是稿件的平台规格与发布打包。
//
// 它只做两件事：告诉界面「这个平台最多几个字」，以及把一篇稿子拼成
// 能直接粘进后台的一段文本。
//
// 为什么值得单独一个包：平台限制是会被改的外部事实（小红书改过一次标题上限），
// 散落在提示词、界面校验、复制逻辑里的三份副本迟早对不上，而对不上的后果
// 是用户复制出去才发现被截掉半句话。集中一处，改一个数，三处同时生效。
package draft

import (
	"fmt"
	"strings"
)

// 两种稿件形态。平台给默认值，不强绑：公众号也能发短的，
// 小红书也能发长图文，用户点的那个才是真的。
const (
	Long  = "long"
	Short = "short"
)

// FormatLabel 是给界面看的中文名。
func FormatLabel(f string) string {
	if f == Short {
		return "口播稿"
	}
	return "图文"
}

// Spec 是一个平台的硬限制。
//
// 0 表示不限。用 0 而不是 -1：平台限制天然是「最多多少」，
// 没有上限就是 0，读代码的人不需要记一个特殊负数。
type Spec struct {
	Key      string `json:"key"`
	Name     string `json:"name"`
	Format   string `json:"format"`   // 该平台默认形态
	TitleMax int    `json:"titleMax"` // 标题上限（字符）
	BodyMax  int    `json:"bodyMax"`  // 正文上限（字符）
	TagMax   int    `json:"tagMax"`   // 标签最多几个
	Where    string `json:"where"`    // 复制之后去哪儿粘
	Note     string `json:"note"`     // 这个平台特有的坑
}

// specs 是唯一一份平台事实表。
//
// 数字来源：小红书标题 20 字、正文 1000 字、话题 10 个；
// 公众号标题 64 字、正文 2 万字；抖音视频描述 1000 字、
// 首屏折叠在 55 字左右、话题 5 个。
var specs = []Spec{
	{
		Key: "xiaohongshu", Name: "小红书", Format: Long,
		TitleMax: 20, BodyMax: 1000, TagMax: 10,
		Where: "创作中心 → 发布笔记 → 上传图片后粘贴",
		Note:  "标题超过 20 字会在信息流里被截断，钩子必须挤在前 20 个字里",
	},
	{
		Key: "wechat", Name: "公众号", Format: Long,
		TitleMax: 64, BodyMax: 20000, TagMax: 3,
		Where: "公众号后台 → 新的创作 → 图文消息",
		Note:  "公众号没有话题标签，末尾那三个词当关键词用，别加 #",
	},
	{
		Key: "douyin", Name: "抖音", Format: Short,
		TitleMax: 55, BodyMax: 1000, TagMax: 5,
		Where: "创作者中心 → 发布视频 → 粘贴文案",
		Note:  "描述前 55 字决定别人点不点开，前 3 秒台词决定他划不划走",
	},
}

// All 返回平台清单，界面直接用它渲染。
func All() []Spec {
	out := make([]Spec, len(specs))
	copy(out, specs)
	return out
}

// Get 按 key 取平台。未知 key 返回 false，调用方要自己决定退回哪个。
func Get(key string) (Spec, bool) {
	for _, s := range specs {
		if s.Key == key {
			return s, true
		}
	}
	return Spec{}, false
}

// 口播稿的字数口径。
//
// 不能用平台的「描述 1000 字」来管台词：抖音那条上限说的是文案框，
// 而口播的物理限制是时长——中文口播每秒约 4.5 个字，90 秒就是 400 字，
// 再长就得二倍速剪，二倍速的口播没人听。
const (
	SpeechMinRunes = 260
	SpeechMaxRunes = 380
	runesPerSecond = 4.5
)

// SpeechSeconds 估算口播时长，取整到秒。
func SpeechSeconds(body string) int {
	n := runeLen(body)
	if n == 0 {
		return 0
	}
	return int(float64(n)/runesPerSecond + 0.5)
}

// Issue 是一条越界提醒。
//
// 只提醒、不阻止：用户可能就是要写长标题，截断与否他自己承担。
// 但必须提前说清会被怎么截——「超了 6 个字」和「第 21 个字开始看不见」
// 是两件事，后者他才能自己决定删哪个词。
type Issue struct {
	Field string `json:"field"` // title / body / tags
	Text  string `json:"text"`
}

// Check 按平台规格检查一篇稿子。format 为口播时，正文按「能不能在
// 90 秒内说完」判，而不是按平台的文案框上限——那两个数管的是两件事。
func Check(sp Spec, format, title, body string, tags []string) []Issue {
	var out []Issue

	if n := runeLen(title); sp.TitleMax > 0 && n > sp.TitleMax {
		out = append(out, Issue{
			Field: "title",
			Text: fmt.Sprintf("标题 %d 字，%s只显示前 %d 字，后面 %d 个字看不见",
				n, sp.Name, sp.TitleMax, n-sp.TitleMax),
		})
	}
	switch n := runeLen(body); {
	case format == Short && n > SpeechMaxRunes:
		out = append(out, Issue{
			Field: "body",
			Text: fmt.Sprintf("台词 %d 字，约 %d 秒，超过 90 秒了。念不完的部分会被二倍速剪掉，没人听完",
				n, SpeechSeconds(body)),
		})
	case format == Short && n > 0 && n < SpeechMinRunes:
		out = append(out, Issue{
			Field: "body",
			Text: fmt.Sprintf("台词只有 %d 字，约 %d 秒，撑不到 60 秒。要么加一个具体细节，要么这选题还不够",
				n, SpeechSeconds(body)),
		})
	case format != Short && sp.BodyMax > 0 && n > sp.BodyMax:
		out = append(out, Issue{
			Field: "body",
			Text: fmt.Sprintf("正文 %d 字，超过%s %d 字上限，发不出去，得删 %d 字",
				n, sp.Name, sp.BodyMax, n-sp.BodyMax),
		})
	}
	if sp.TagMax > 0 && len(tags) > sp.TagMax {
		out = append(out, Issue{
			Field: "tags",
			Text: fmt.Sprintf("标签 %d 个，超过%s %d 个上限，多的会被丢掉",
				len(tags), sp.Name, sp.TagMax),
		})
	}
	if strings.TrimSpace(title) == "" {
		out = append(out, Issue{Field: "title", Text: "还没有标题"})
	}
	if strings.TrimSpace(body) == "" {
		out = append(out, Issue{Field: "body", Text: "正文还是空的"})
	}
	return out
}

// Pack 是复制出去的东西。字段分开给，是因为有些后台要分栏填，
// 全塞一段文本里用户还得自己切。
type Pack struct {
	Title string   `json:"title"`
	Body  string   `json:"body"`
	Tags  []string `json:"tags"`
	Text  string   `json:"text"` // 一次粘进正文的整段（正文 + 标签）
}

// Build 拼出发布包。
//
// 标签的处理按平台分岔：小红书和抖音把话题写在正文末尾，粘一次就够；
// 公众号没有话题体系，硬加 # 会显示成正文里的井号，所以只给词、不加井号。
func Build(sp Spec, title, body string, tags []string) Pack {
	tags = cleanTags(tags)
	p := Pack{Title: strings.TrimSpace(title), Body: strings.TrimSpace(body), Tags: tags}

	joined := make([]string, 0, len(tags))
	for _, t := range tags {
		if sp.Key == "wechat" {
			joined = append(joined, t)
		} else {
			joined = append(joined, "#"+t)
		}
	}

	parts := []string{p.Body}
	if len(joined) > 0 {
		joiner := " "
		if sp.Key == "wechat" {
			joiner = "、"
		}
		parts = append(parts, strings.Join(joined, joiner))
	}
	p.Text = strings.TrimSpace(strings.Join(parts, "\n\n"))
	return p
}

// cleanTags 去掉 # 前缀和空白。
//
// 模型经常自己加 #，用户也可能从别的地方粘带井号的标签。
// 这里统一剥掉，输出时再按平台加回去——不然会拼出「##职场」。
func cleanTags(tags []string) []string {
	out := make([]string, 0, len(tags))
	seen := make(map[string]bool, len(tags))
	for _, t := range tags {
		t = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(t), "#"))
		t = strings.TrimSpace(strings.TrimPrefix(t, "＃"))
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return out
}

func runeLen(s string) int { return len([]rune(strings.TrimSpace(s))) }
