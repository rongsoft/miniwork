package ai

import (
	"encoding/json"
	"fmt"
	"strings"
)

// 成稿场景。与拆解、起标题共用 brief，但提示词完全不同：
// 前两个是在「看」，这个是在「写」。
//
// 这里所有的约束都来自同一个判断：模型写稿最擅长的两件事是
// 说漂亮话和编故事，而这两件事在自媒体里都是负资产。所以提示词里
// 一半的篇幅在禁两件事——禁评价词、禁编经历。
const (
	FormatLong  = "long"
	FormatShort = "short"
)

// Segment 是口播稿的一个镜头。
//
// 三个字段分开，是因为拍摄时缺一不可：不知道拍什么就摆不了机位，
// 不知道说到哪就不知道怎么剪。合成一段话反而没法用。
type Segment struct {
	At   string `json:"at"`   // 时间区间，如 0-3s
	Shot string `json:"shot"` // 画面：镜头里拍得到的东西
	Line string `json:"line"` // 台词：嘴里说的话
}

// Draft 是一篇成稿。
//
// Body 与 Segments 对图文长文是「有前者、无后者」，
// 对口播稿是「两者都有」：Body 是整段台词，方便一键复制，
// Segments 是分镜，方便照着拍。重复存一份是有意的——
// 用户复制时不想看到「镜头 2 画面：...」。
type Draft struct {
	Title    string    `json:"title"`
	Hook     string    `json:"hook"`
	Body     string    `json:"body"`
	Tags     []string  `json:"tags"`
	Segments []Segment `json:"segments"`
	Note     string    `json:"note"`
}

// DraftInput 是写稿需要的全部输入。
//
// 把平台上限传进来，是为了让模型第一次就写进框里。
// 不传也行，但那等于让用户收到一篇超长稿再自己删——
// 而删的时候他不知道该删哪句，因为每句都是模型认真写的。
type DraftInput struct {
	Topic    string
	Platform string
	Format   string
	Material string   // 用户自己的经历、观点、数据，选填
	Skeleton []string // 从爆款拆解里拿到的骨架，选填
	TitleMax int
	BodyMax  int
	TagMax   int
}

// BuildDraftPrompt 拼成稿提示词。
func BuildDraftPrompt(in DraftInput, b Brief) (system, user string) {
	if in.Format == FormatShort {
		system = shortDraftSystem(in)
	} else {
		system = longDraftSystem(in)
	}

	var sb strings.Builder
	sb.WriteString("作者信息：\n")
	writeBrief(&sb, b)

	fmt.Fprintf(&sb, "\n目标平台：%s\n", in.Platform)
	sb.WriteString("选题：" + strings.TrimSpace(in.Topic) + "\n")

	if len(in.Skeleton) > 0 {
		sb.WriteString("\n作者想用的结构（从别的爆款里拆出来的，按这个顺序推进，但不要照抄句子）：\n")
		for i, s := range in.Skeleton {
			fmt.Fprintf(&sb, "%d. %s\n", i+1, strings.TrimSpace(s))
		}
	}

	if m := strings.TrimSpace(in.Material); m != "" {
		sb.WriteString("\n作者自己的素材（必须用上，这是这篇稿子里唯一别人没有的东西）：\n")
		sb.WriteString(clip(m, 2000) + "\n")
	} else {
		sb.WriteString("\n（作者没提供个人素材。此时只能写读者普遍经历过的场景，" +
			"不许编造具体的人名、公司、数字、对话来冒充亲身经历。）\n")
	}
	return system, sb.String()
}

// longDraftSystem 是图文长文的写作约束。
//
// 四条硬约束各自对应一种真实失败：
//   - 不许编经历：模型写「我朋友小王」是常态，用户原样发出去就是造假
//   - 第一句必须具体：不写这条，八篇稿子全从「在这个时代」开始
//   - 一段不超过三行：手机屏幕上超过三行就是一块砖，没人读
//   - 结尾要具体问题：「你怎么看」是最没用的收尾，读者答不出来
func longDraftSystem(in DraftInput) string {
	titleMax := numOr(in.TitleMax, 20)
	bodyMax := numOr(in.BodyMax, 1000)
	tagCount := tagCountHint(in.TagMax)

	return fmt.Sprintf(`你是一个自媒体作者，替作者把选题写成一篇可以直接发出去的稿子。
你不是在给建议，是在交稿：你写下的每个字，作者都会原样贴到平台上。

四条硬约束：
1. 只用作者提供的信息。他没说的经历、数据、公司、人名、对话，一律不许编。
   缺细节就写读者也经历过的通用场景，绝不要造一个「我朋友」来当证据。
2. 第一句必须扎在具体的东西上：一个动作、一个数字、一句当时听到的话。
   禁止「大家好」「在这个时代」「最近很多人问我」「今天想聊聊」这类开场——
   它们等于把最贵的版面花在热身。
3. 段落要成段。每段 2-4 句、60-120 字，全文 5-8 段。
   只有两处允许单句成段：开头那句钩子、结尾那句行动。
   其余地方一句一行，读起来是标语墙，不是一个人在说话。
4. 结尾给读者一个能动手回答的问题，或者一件能立刻做的小事。
   禁止「你怎么看」「欢迎在评论区讨论」——那是把球踢回去，不是收尾。

写法要求：
- 通篇以「你」起头是机器味最重的地方。同一段里连着两句都从「你」开始就要改：
  换成「我」「我们这行的人」「有人」，或者直接陈述事实。
- 不要用这些词：沉没成本、情绪价值、底层逻辑、认知、格局、内耗、闭环、
  赛道、赋能、成年人、扎心、清醒。它们一出现，这句就是别人的话，不是他的事。
- 用作者的语气。不要用「我们」来指代读者，那会让所有人都不觉得在说自己。
- 不要「首先/其次/最后/综上所述」，那是提纲不是文章。
- 不要堆四字词、不要排比。一句话能说完就说完。
- 正文用空行分段，不要用 markdown 符号（#、-、**），平台不识别，纯文本更干净。

长度：正文 %d 字以内，写到上限的七八成就停。把一件事讲透比把版面填满重要，
不要为凑字数铺排。
标题：%d 字以内。这不是复述选题——不许用问句，不许以选题的前 8 个字开头，
也不许只把「该不该」改成「要不要」来假装改过（这一条被逮到过好几次）。
标题要说正文的结论，或者正文里最具体的那个场景。
标签：%s。要具体到有人真的会搜，不要「职场干货」「打工人日常」这种大词。
不要给标签加 #。

只输出 JSON 对象，不要解释，不要 markdown 代码块。格式：
{"title":"标题","body":"正文，段落之间用\n\n分隔","tags":["标签1","标签2"],"note":"这一稿的角度，一句话说清它和普通写法的区别"}`,
		bodyMax, titleMax, tagCount)
}

// shortDraftSystem 是口播稿的写作约束。
//
// 字数是硬约束而不是建议：中文口播每秒约 4.5 个字，400 字就是 90 秒，
// 超了就得二倍速剪，二倍速的口播没人听。
func shortDraftSystem(in DraftInput) string {
	titleMax := numOr(in.TitleMax, 55)
	tagCount := tagCountHint(in.TagMax)

	return fmt.Sprintf(`你是一个短视频口播稿写手，替作者写一条 60-90 秒的口播。

四条硬约束：
1. 第一句就是钩子，20 字以内，必须是具体的事或具体的反常识结论。
   禁止自我介绍、禁止「今天我们来聊」、禁止「大家好」。
2. 全文 260-380 字。中文口播每秒约 4.5 个字，60 秒就是 270 字。
   写到 260 字以下，通常不是没话说，是说得太省：把作者素材里的一个细节讲透——
   当时什么场景、他具体做了什么、结果如何，三句话能讲完的事别用一句带过。
   但绝不靠空话凑数，也不许编造他没给的经历。超过 400 字一定说不完，得删。
3. 分 4-6 个镜头。每个镜头写清「画面拍什么」和「嘴里说什么」。
   画面必须是手机拍得到的：不要写「展示数据图表」，写「把简历拍在桌上」。
4. 台词是口语：一句不超过 25 字，不许出现「接下来」「综上所述」「由此可见」。
   写完默念一遍，念着打结的地方就是该改的地方。

结尾给一句具体的行动指令：让观众做一件马上能做的小事，
不要「点赞关注」，那是求他，不是给他东西。

时间轴由台词决定，不是反过来：从 0 秒开始，每段时长 = 这段台词的字数 ÷ 4.5，
累加到最后一段结束。最后那个数字就是视频长度，它应该落在 60-90 秒之间。
不要为了凑时间轴去删台词，也不要为了凑时长把一句话拆成三段说。

不许编造作者没提供的经历与数据。他没给素材时，写观众普遍经历过的场景。

标题（视频描述）：%d 字以内，前 20 字就要能独立看懂。
标签：%s。真实存在的词，不要造。
不要给标签加 #。

只输出 JSON 对象，不要解释，不要 markdown 代码块。格式：
{"title":"视频标题","hook":"前 3 秒那句台词","segments":[{"at":"0-3s","shot":"画面拍什么","line":"台词"},{"at":"3-12s","shot":"画面","line":"台词"}],"body":"完整台词，段落之间用\n\n分隔，供复制","tags":["标签1","标签2"],"note":"这一稿的角度，一句话"}`,
		titleMax, tagCount)
}

func numOr(n, fallback int) int {
	if n > 0 {
		return n
	}
	return fallback
}

// tagCountHint 把标签上限翻译成一句人话。
// 上限为 0（公众号这种没有话题体系的）时说「三个关键词」，不说「0 个」。
func tagCountHint(max int) string {
	switch {
	case max <= 0:
		return "3 个关键词（这个平台没有话题标签，给的词会当文末关键词用）"
	case max == 1:
		return "1 个"
	case max <= 3:
		return fmt.Sprintf("%d 个", max)
	default:
		// 平台上限是硬限制，不是目标。小红书能放十个，一次堆满十个泛词，
		// 整篇立刻像个营销号——三个准的比八个泛的有用。
		return "3-5 个"
	}
}

// ParseDraft 解析成稿。
//
// 容忍两种常见退化：body 空但 segments 有（模型把台词只写在分镜里），
// 以及 tags 给成逗号分隔的字符串。两种都修好，不让用户看到一篇空稿。
func ParseDraft(content string) (Draft, error) {
	raw, err := extractObject(content)
	if err != nil {
		return Draft{}, err
	}
	var r struct {
		Title    any `json:"title"`
		Hook     any `json:"hook"`
		Body     any `json:"body"`
		Tags     any `json:"tags"`
		Segments any `json:"segments"`
		Note     any `json:"note"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return Draft{}, fmt.Errorf("成稿结果不是合法 JSON: %w", err)
	}

	d := Draft{
		Title: strings.TrimSpace(toStr(r.Title)),
		Hook:  strings.TrimSpace(toStr(r.Hook)),
		Body:  strings.TrimSpace(toStr(r.Body)),
		Note:  strings.TrimSpace(toStr(r.Note)),
		Tags:  parseTags(r.Tags),
	}
	d.Segments = parseSegments(r.Segments)

	if d.Body == "" && len(d.Segments) > 0 {
		lines := make([]string, 0, len(d.Segments))
		for _, s := range d.Segments {
			if s.Line = strings.TrimSpace(s.Line); s.Line != "" {
				lines = append(lines, s.Line)
			}
		}
		d.Body = strings.Join(lines, "\n\n")
	}
	if strings.TrimSpace(d.Body) == "" {
		return Draft{}, fmt.Errorf("模型没写出正文: %s", clip(content, 80))
	}
	if d.Title == "" {
		// 标题可以用正文首句兜底：没有标题的稿子没法发，
		// 而这里报错让用户重生成一次，代价比让他自己起个标题大。
		d.Title = firstLine(d.Body)
	}
	return d, nil
}

func parseTags(v any) []string {
	var raw []string
	switch t := v.(type) {
	case []any:
		for _, x := range t {
			if s := strings.TrimSpace(toStr(x)); s != "" {
				raw = append(raw, s)
			}
		}
	case string:
		for _, s := range strings.FieldsFunc(t, func(r rune) bool {
			return r == ',' || r == '、' || r == '，' || r == ' '
		}) {
			if s = strings.TrimSpace(s); s != "" {
				raw = append(raw, s)
			}
		}
	}
	// 去重并剥掉模型自带的 #，否则拼出去会变成 ##职场。
	seen := map[string]bool{}
	out := make([]string, 0, len(raw))
	for _, s := range raw {
		s = strings.TrimSpace(strings.TrimLeft(s, "#＃"))
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func parseSegments(v any) []Segment {
	list, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]Segment, 0, len(list))
	for _, x := range list {
		m, ok := x.(map[string]any)
		if !ok {
			continue
		}
		seg := Segment{
			At:   strings.TrimSpace(toStr(m["at"])),
			Shot: strings.TrimSpace(toStr(m["shot"])),
			Line: strings.TrimSpace(toStr(m["line"])),
		}
		if seg.Line == "" && seg.Shot == "" {
			continue
		}
		out = append(out, seg)
	}
	return out
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i > 0 {
		s = s[:i]
	}
	return clip(s, 30)
}
