package ai

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// 创作场景。与打分场景分开是因为两件事对模型的要求相反：
// 打分要稳定、要克制（temperature 低、禁止发挥）；
// 创作要发散、要给得出具体句子（同一批候选中至少要有几个不一样）。
const (
	SceneTeardown = "teardown"
	SceneTitle    = "title"
)

// Teardown 是一条爆款的拆解结果。
//
// 结构是「六行短字段 + 一句可借鉴点」，不是六段分析。
// 前一版每条字段都是一段话，用户反馈「太繁琐」——问题不在块多，
// 在于每块都要读。人拆爆款是扫一眼就要拿去用，不是读一篇分析报告。
//
// 所以硬约束都写在提示词里：每个字段一行、不超过 20 字、必须指到动作。
// Structure 仍然是一个可以直接拿去当写稿骨架的顺序串（「疑问开场→3个方法→提问收尾」），
// 它既是给人看的一行，也是给模型用的提纲——一句话两用，比单独再存一份 steps 省事。
type Teardown struct {
	Title     string   `json:"title"`     // 标题亮点：它标题做对了哪个动作
	Cover     string   `json:"cover"`     // 封面设计：视觉上靠什么拦人
	Structure string   `json:"structure"` // 内容结构：推进公式，可直接当骨架
	Engage    string   `json:"engage"`    // 互动引导：结尾用什么动作换互动
	Tone      string   `json:"tone"`      // 语气风格：口吻与情绪（原「情绪」并进来）
	Topics    []string `json:"topics"`    // 适合话题：能直接挂的标签
	Learn     string   `json:"learn"`     // 可借鉴点：下次怎么写能照做，整张卡最重要的一行
	Category  string   `json:"category"`  // 分类：内容类型，写法「A/B」
}

// Title 是一个候选标题。
type Title struct {
	Text string `json:"text"`
	Hook string `json:"hook"` // 用了哪种钩子
	Note string `json:"note"` // 这么写的作用，一句话
}

// BuildTeardownPrompt 拼爆款拆解提示词。
//
// 这段文案是产品资产，改动当改产品对待。每条硬约束都对应一次具体的翻车：
//   - 限字数：不限就会写成分析报告，用户扫不动（上一版的真实反馈）
//   - 禁止评价性词汇：模型天生爱说「很有共鸣」，这话对写稿零帮助
//   - 要求引用原文：不引用就一定是套模板，引用了才倒逼它真的读
//   - 没给正文就说看不出来：宁可少说，也不能编一段不存在的封面设计
func BuildTeardownPrompt(title, body string, b Brief) (system, user string) {
	system = `你是一个爆款笔记拆解师。作者拿别人的爆款来问你「它好在哪」，目的是照着做出同款。他不读分析报告，他扫一眼就要能用。

只输出一个 JSON 对象，不要解释，不要 markdown 代码块：
{"title":"标题亮点","cover":"封面设计","structure":"内容结构","engage":"互动引导","tone":"语气风格","topics":["话题1","话题2"],"learn":"可借鉴点","category":"分类"}

每个字段怎么写（逐条照做，这是全部要求）：
- title 标题亮点：它标题做对了哪个动作。不超过 20 字。要写成「数字+身份，把结果前置」这种动作描述。
- cover 封面设计：封面靠什么把人拦下来（大字、对比色、真人、前后对比…）。不超过 20 字。没看到封面就写「未提供封面，看不出」，不许编。
- structure 内容结构：用箭头写推进公式，3-4 段，总长不超过 24 字。例：疑问开场→3个方法→提问收尾。这一行会被直接拿去当写稿骨架，必须是能照抄的顺序。
- engage 互动引导：结尾用什么具体动作换留言/收藏。不超过 20 字。
- tone 语气风格：口吻加情绪，写「冷静复盘，带自嘲」，不要「很有感染力」。
- topics 适合话题：2-3 个能直接挂的标签，每个 2-6 字。
- learn 可借鉴点：一句话说清「我下次怎么写能照做」，不超过 40 字。这是整张卡最重要的一行，不要重复上面任何一条。
- category 分类：内容类型，写成「A/B」，如「干货/教程」「职场/成长」「生活/日常」。

硬约束：
1. 每个字段都要指到具体位置或具体动作。禁止「引发共鸣」「干货满满」「角度新颖」「值得学习」这类评价词。
2. 引用原文时用「」包住，只摘关键几个字。
3. 不要打分、不要列建议清单、不要写「为什么有效」的长段落。用户要的是照做，不是理解。`

	var sb strings.Builder
	sb.WriteString("作者信息：\n")
	writeBrief(&sb, b)
	sb.WriteString("\n要拆的内容：\n")
	sb.WriteString("标题：" + strings.TrimSpace(title) + "\n")
	if body = strings.TrimSpace(body); body != "" {
		sb.WriteString("正文：\n" + clip(body, 2000) + "\n")
	} else {
		sb.WriteString("（作者只给了标题，没给正文，也没给封面。只能拆标题层：cover、structure、engage 一律写「未提供正文，看不出」，不许猜。）\n")
	}
	return system, sb.String()
}

// BuildTitlePrompt 拼标题生成提示词。
//
// hooks 是用户勾选的钩子类型。要求「每种钩子都要真的不一样」是关键：
// 不写这句，模型会给出八个同义句，用户看着很热闹，实际只有一个选择。
//
// 实测补强（2026-11-17）：只说「要不同」，模型交上来的还是八条同义词——
// 全是「35岁被优化，40天拿到更好offer」的变体，只换了数字和语序。
// 原因是「不同」对模型是个模糊词，它以为改个说法就算不同。所以这里改成
// 先分配角度（一条一个钩子）、再给一条可执行的自查（只换了数字就重写）。
func BuildTitlePrompt(seed string, hooks []string, b Brief) (system, user string) {
	system = `你是一个自媒体标题写手。作者给你一个想讲的内容，你给出 8 个候选标题。

要求：
1. 每个标题不超过 24 个汉字。超过就不是标题，是摘要。
2. 八条必须是八个不同的角度，不是八句同义话。做法：先定 8 个角度，一条占一个，
   角度从这几类里挑（可以不用满，但不许两条挤在同一类）：
   身份（写给谁）、数字（具体到几）、反常识（和常识相反）、损失（不这么做会失去什么）、
   时间（多久之内）、对象（针对某类具体的人）、对比（两种做法的差别）、提问（读者答得上来的问题）。
3. 自查，不合格就重写：把八条并排读一遍，任意两条只要只是换了数字、形容词、标点或语序，
   就算不合格。八条里最多 2 条能用「我」开头，最多 2 条用「你」开头。
4. 禁止套话：不要「震惊」「必看」「干货满满」「建议收藏」，不要用感叹号堆情绪。
5. 具体优于抽象。有数字、有身份、有具体场景的标题优先。
6. 不编造内容里没有的事实。作者没说的数据不要自己加。
7. 每条标注它用的钩子和一句作用说明。作用说明要说「它让读者产生什么动作」，不要说「很吸引人」。

只输出 JSON 数组，不要解释，不要 markdown 代码块。格式：
[{"text":"标题本身","hook":"钩子类型","note":"这条为什么能让人点"}]`

	var sb strings.Builder
	sb.WriteString("作者信息：\n")
	writeBrief(&sb, b)
	if len(hooks) > 0 {
		sb.WriteString("\n想要的钩子：" + strings.Join(hooks, "、") + "\n")
	}
	sb.WriteString("\n要写的内容：\n" + clip(strings.TrimSpace(seed), 800) + "\n")
	return system, sb.String()
}

// writeBrief 把作者信息写进 user 段。人设缺失时明说，避免模型自己编一个作者。
func writeBrief(sb *strings.Builder, b Brief) {
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
		sb.WriteString("- （作者还没填写人设，不要假设他的身份和经历）\n")
	}
}

// ParseTeardown 解析拆解结果。容忍模型加话、加代码块。
//
// 顺手做一遍「卡片化」清洗：折叠换行、去掉模型爱加的项目符号和序号。
// 不清洗的话，一条 20 字的字段里带个换行，卡片就会塌成两行——这恰恰是
// 这次改造要消掉的那种「繁琐」。
func ParseTeardown(content string) (Teardown, error) {
	raw, err := extractObject(content)
	if err != nil {
		return Teardown{}, err
	}
	var t Teardown
	if err := json.Unmarshal(raw, &t); err != nil {
		return Teardown{}, fmt.Errorf("拆解结果不是合法 JSON: %w", err)
	}
	t.Title = cardLine(t.Title, 48)
	t.Cover = cardLine(t.Cover, 48)
	t.Structure = cardLine(t.Structure, 56)
	t.Engage = cardLine(t.Engage, 48)
	t.Tone = cardLine(t.Tone, 48)
	t.Learn = cardLine(t.Learn, 90)
	t.Category = cardLine(t.Category, 20)

	topics := make([]string, 0, len(t.Topics))
	for _, s := range t.Topics {
		if s = cardLine(s, 12); s != "" {
			topics = append(topics, s)
		}
		if len(topics) == 3 {
			break
		}
	}
	t.Topics = topics

	// 判「有没有拆出东西用」：六个字段里至少三个有内容，且必须有结构和可借鉴点之一。
	// 只判一个字段太松——模型偶尔会只回一句「这条很好」也算通过。
	filled := 0
	for _, s := range []string{t.Title, t.Cover, t.Structure, t.Engage, t.Tone, t.Learn} {
		if s != "" {
			filled++
		}
	}
	if filled < 3 || (t.Structure == "" && t.Learn == "") {
		return Teardown{}, fmt.Errorf("模型没有给出可用内容: %s", clip(content, 80))
	}
	return t, nil
}

// cardLine 把一段模型输出压成卡片上的一行。
//
// 换行、连续空白、行首的项目符号和「1. 」这类序号在外层已经有位置了，
// 留在字段里只会让卡片参差不齐。超长就截断——这一版的产品决定是「一行」，
// 与其让用户读一段长文，不如截断后让他自己再点一次。
func cardLine(s string, max int) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	lines := strings.Split(strings.ReplaceAll(s, "\r", ""), "\n")
	s = ""
	for _, ln := range lines {
		ln = strings.TrimSpace(cardPrefix.ReplaceAllString(ln, ""))
		if ln == "" || strings.HasSuffix(ln, "：") || strings.HasSuffix(ln, ":") {
			// 「标题亮点：」这种只有标签的行跳过去，取真正的内容行。
			continue
		}
		s = ln
		break
	}
	if s == "" {
		return ""
	}
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > max {
		s = strings.TrimRight(string(r[:max]), "，,、；; ") + "…"
	}
	return s
}

// cardPrefix 吃掉行首的项目符号、序号。模型不加这些反而少见。
var cardPrefix = regexp.MustCompile(`^(?:[-*·•]+\s*|\d{1,2}[.、)）]\s*|[①-⑩]\s*)`)

// ParseTitles 解析标题候选。
func ParseTitles(content string) ([]Title, error) {
	raw, err := extractArray(content)
	if err != nil {
		return nil, err
	}
	var raws []struct {
		Text any `json:"text"`
		Hook any `json:"hook"`
		Note any `json:"note"`
	}
	if err := json.Unmarshal(raw, &raws); err != nil {
		return nil, fmt.Errorf("标题结果不是合法 JSON: %w", err)
	}
	out := make([]Title, 0, len(raws))
	for _, r := range raws {
		text := strings.TrimSpace(toStr(r.Text))
		if text == "" {
			continue
		}
		out = append(out, Title{
			Text: text,
			Hook: strings.TrimSpace(toStr(r.Hook)),
			Note: strings.TrimSpace(toStr(r.Note)),
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("模型没有给出任何标题: %s", clip(content, 80))
	}
	return out, nil
}

// extractObject 从回复里抠出第一个 JSON 对象。
// 模型经常在 JSON 前后加「好的，我来分析一下：」这类话。
func extractObject(s string) ([]byte, error) {
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("模型回复里找不到 JSON 对象: %s", clip(s, 80))
	}
	return []byte(s[start : end+1]), nil
}
