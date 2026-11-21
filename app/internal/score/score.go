// Package score 用可解释的规则给候选内容打分。
//
// 先用规则而不是模型，理由有两个：
//  1. 零成本、毫秒级，可以让「首启动 90 秒内出结果」这个承诺先成立；
//  2. 规则给出的是结构化理由，能直接显示给用户看；模型给的理由不可控。
//
// 后续接 AI 打分时，这一层保留为兜底与校准基线。
package score

import (
	"fmt"
	"math"
	"strings"
	"time"

	"rongsoft/miniwork/app/internal/source"
)

// Factor 是单个评分维度，Detail 会原样显示给用户。
type Factor struct {
	Name   string  `json:"name"`
	Points float64 `json:"points"`
	Max    float64 `json:"max"`
	Detail string  `json:"detail"`
}

// Result 是一条内容的评分结果。
type Result struct {
	Score   int      `json:"score"`
	Reason  string   `json:"reason"`
	Factors []Factor `json:"factors"`

	// NicheHit 是命中的赛道词个数。偏好加分层要用它判断
	// 「这条是不是跑题内容」——跑题的不许拿偏好分。
	NicheHit int `json:"nicheHit"`

	// Vetoed 表示这条被人设禁忌否决过。
	// 否决必须压过偏好：禁忌是用户明确说「不碰」的，
	// 就算它同时命中十个偏好词也不能翻身。
	Vetoed bool `json:"vetoed"`
}

// nicheKeywords 是赛道匹配词库。命中越多，赛道相关分越高。
//
// 每个赛道原来只有 7-12 个词，覆盖不到真实标题的说法：一条「绩效面谈时这3句话
// 千万别先说」明明是职场内容，一个词都没命中，赛道相关 0 分，加上热度和时效
// 也很难过 50 分线——于是被当成噪音扔掉。用户看到的结果就是「刷了半天没几条」，
// 而流水线那边还很无辜地报「取到 22 条」。
//
// 补词的标准是「只有这个赛道会这么说」：绩效、转岗、周报只出现在职场，
// 备菜、剩饭只出现在美食。不收「分享」「建议」这类谁的标题都能用的词——
// 那种词加进来，噪音也会跟着涨分，等于把过滤线一起调没了。
var nicheKeywords = map[string][]string{
	"职场": {"裁员", "跳槽", "领导", "升职", "面试", "加班", "试用期", "同事", "汇报", "优化", "HR", "涨薪",
		"绩效", "转岗", "离职", "实习", "提拔", "部门", "老板", "职场", "入职", "裸辞", "工资"},
	"副业": {"副业", "收入", "下班", "月入", "主业", "爱好", "赚钱",
		"接单", "兼职", "变现", "带货", "客户", "私活", "睡后收入"},
	"理财": {"存钱", "理财", "基金", "存款", "记账", "工资", "杠杆", "爆仓", "10万",
		"存下", "省下", "支出", "保险", "定投", "收益率", "信用卡", "应急金", "房贷", "月薪"},
	"育儿": {"孩子", "早教", "陪写", "报班", "绘本", "哭闹", "二胎", "磨蹭", "家长",
		"幼儿园", "写作业", "上学", "沉迷手机", "吼", "内向", "双职工", "夸奖"},
	"健身": {"减脂", "深蹲", "私教", "马甲线", "增肌", "有氧", "腰痛", "跑步", "久坐",
		"健身", "体脂", "卷腹", "器械", "力量", "运动", "掉秤", "练"},
	"美食": {"快手菜", "冰箱", "调料", "空气炸锅", "做饭", "减脂餐", "红烧肉", "网红",
		"备菜", "剩饭", "面条", "早餐", "外卖", "买菜", "厨房", "一锅", "带饭"},
	"穿搭": {"穿搭", "显高", "显瘦", "配色", "通勤", "断舍离", "梨形", "衣柜",
		"衣服", "衬衫", "基础款", "小个子", "微胖", "腰线", "配饰", "鞋子", "阔腿裤"},
	"AI工具": {"AI", "prompt", "周报", "杂活", "工具", "代码", "实习生",
		"模型", "提示词", "生成", "效率", "自动化"},
}

// painWords 是痛点词，命中说明标题戳到了具体处境。
var painWords = []string{"被裁", "焦虑", "亏", "后悔", "踩坑", "省钱", "避坑", "失败", "放弃", "真相"}

// Score 对一条内容按赛道打分，满分 100。
func Score(h source.Hotspot, niche string, now time.Time) Result {
	var fs []Factor

	// 1) 赛道相关 40 分：命中 2.5 个词封顶，1 个词约 16 分。
	hit := 0
	var hitWords []string
	for _, kw := range nicheKeywords[niche] {
		if strings.Contains(h.Title, kw) {
			hit++
			hitWords = append(hitWords, kw)
		}
	}
	nichePts := math.Min(float64(hit)/2.5, 1.0) * 40
	nicheDetail := "未命中赛道词"
	if hit > 0 {
		nicheDetail = "命中「" + strings.Join(hitWords, "、") + "」"
	}
	fs = append(fs, Factor{"赛道相关", nichePts, 40, nicheDetail})

	// 2) 热度 30 分：1000 → 0，100 万 → 30，对数映射。
	heatPts := 0.0
	if h.Heat > 1000 {
		heatPts = math.Min((math.Log10(float64(h.Heat))-3)/3, 1) * 30
	}
	fs = append(fs, Factor{"热度", heatPts, 30, "互动 " + human(h.Heat)})

	// 3) 时效 20 分：6 小时内满分，24 小时 12 分，72 小时归零。
	hours := now.Sub(h.PublishedAt).Hours()
	if hours < 0 {
		hours = 0
	}
	timePts := 0.0
	switch {
	case hours <= 6:
		timePts = 20
	case hours <= 24:
		timePts = 20 - (hours-6)/18*8
	case hours <= 72:
		timePts = 12 - (hours-24)/48*12
	}
	fs = append(fs, Factor{"时效", timePts, 20, ago(hours)})

	// 4) 标题结构 10 分：这一点在规则时代替不了模型，只能近似。
	structPts := 0.0
	var marks []string
	if hasDigit(h.Title) {
		structPts += 3
		marks = append(marks, "含数字")
	}
	if strings.ContainsAny(h.Title, "？?") || strings.Contains(h.Title, "为什么") || strings.Contains(h.Title, "怎么") {
		structPts += 2
		marks = append(marks, "疑问句")
	}
	for _, w := range []string{"却", "反而", "竟然", "原来", "其实", "根本"} {
		if strings.Contains(h.Title, w) {
			structPts += 2
			marks = append(marks, "有反转")
			break
		}
	}
	if strings.Contains(h.Title, "我") || strings.Contains(h.Title, "你") {
		structPts += 2
		marks = append(marks, "第一人称")
	}
	for _, w := range painWords {
		if strings.Contains(h.Title, w) {
			structPts += 1
			marks = append(marks, "痛点词")
			break
		}
	}
	if structPts > 10 {
		structPts = 10
	}
	structDetail := "平铺直叙"
	if len(marks) > 0 {
		structDetail = strings.Join(marks, " + ")
	}
	fs = append(fs, Factor{"标题结构", structPts, 10, structDetail})

	total := 0.0
	for _, f := range fs {
		total += f.Points
	}

	// 赛道无关的内容封顶 42：热度再高也不能上榜。
	// 用户要的是"我这条赛道里的选题"，一条完全跑题的爆款对他没有价值，
	// 让它混进列表比少给一条更伤信任。
	if hit == 0 && total > 42 {
		total = 42
	}

	return Result{
		Score:    int(math.Round(total)),
		Reason:   reason(fs),
		Factors:  fs,
		NicheHit: hit,
	}
}

// NicheWords 返回某个赛道的全部关键词。
//
// 记忆层要它做排除：赛道词已经在打分里占了 40 分，
// 若再被当成「你常收的词」加一次，就是同一条证据算了两遍。
func NicheWords(niche string) []string {
	return nicheKeywords[niche]
}

// Prefs 是用户人设里跟打分有关的那部分。
//
// 这里刻意不引用 store.Profile：score 是纯计算包，不该知道数据怎么存。
// 字段只留三个，因为它们各自能被规则用上；tone 这类只能靠模型的就不传进来，
// 传进来却用不上，只会让人以为它在起作用。
type Prefs struct {
	Positioning string
	Audience    string
	Avoid       string
}

// avoidTopics 把「不想碰的话题」翻译成可匹配的触发词。
//
// 用户在 avoid 里写的是一句人话（「不碰裁员赔偿的法律细节」），规则引擎读不懂
// 语义，只能做一件事：认出他提到了哪一类，再按那一类的典型词去扫标题。
//
// 为什么必须有这一层：人设如果只在接上模型之后才生效，那首启动填完它就是死的，
// 用户看不到任何变化，第二次就不会再填。规则能兑现的那部分必须先兑现。
var avoidTopics = map[string][]string{
	"法律": {"仲裁", "赔偿", "劳动法", "起诉", "律师", "维权", "违约金", "合同纠纷"},
	"医疗": {"医院", "医生", "手术", "吃药", "症状", "治疗", "诊断", "偏方"},
	"股票": {"涨停", "跌停", "大盘", "K线", "个股", "抄底", "套牢"},
	"内幕": {"内幕", "内部消息", "爆料", "小道消息"},
}

// avoidPenalty 是命中禁忌的扣分。
//
// 30 分是算出来的，不是拍的：一条 70 分的正常选题扣完落到 40，低于 50 的准入线，
// 进不了当天的列表；而它不会把分数打成负数，用户手动翻历史时仍看得到这条存在过。
const avoidPenalty = 30

// ApplyPrefs 用「你不想碰的东西」给分数加一道否决。
//
// 只做减法，不做加法：加分需要理解语义（这条像不像你的风格），规则做不到；
// 减分只需要关键词命中，规则恰好能做准。诚实地做能做的部分。
func ApplyPrefs(r Result, title string, p Prefs) Result {
	if strings.TrimSpace(p.Avoid) == "" {
		return r
	}
	// 用户选「暂时没有」时，这句话里不能当成禁忌词去扫。
	if strings.Contains(p.Avoid, "暂时没有") && len([]rune(p.Avoid)) <= 6 {
		return r
	}

	for topic, words := range avoidTopics {
		if !strings.Contains(p.Avoid, topic) {
			continue
		}
		for _, w := range words {
			if !strings.Contains(title, w) {
				continue
			}
			r.Score -= avoidPenalty
			if r.Score < 0 {
				r.Score = 0
			}
			r.Reason = fmt.Sprintf("你说过不碰%s，这条涉及「%s」", topic, w)
			r.Vetoed = true
			return r
		}
	}
	return r
}

// 偏好加分的两个数。之所以要封顶，是因为下面的推导。
const (
	// learnedPerWord：命中一个偏好词加多少分。
	learnedPerWord = 4

	// learnedCap：全部命中合计最多加多少分。
	//
	// 8 分是拿分档反推的：赛道 40 / 热度 30 / 时效 20 / 结构 10。
	// 偏好加分要能改变「同一水平里谁排前面」，但不能改变「谁够格上榜」——
	// 若给到 20 分，一条 45 分的内容靠蹭偏好词能顶到 65，直接越过准入线，
	// 那时列表就不再是「你的赛道热点」，而是「你的历史回声」。
	// 8 分小于热度档与时效档之间的差距，只能在相近分数里做微调。
	learnedCap = 8
)

// ApplyLearnt 用「你常收的那类词」给内容加一点分。
//
// 这里有一条结构性约束，比具体加几分更重要：
// **赛道没命中的内容一律不加分**（NicheHit == 0 直接返回）。
//
// 偏好是从你过去的收下里学的，它天然有让视野变窄的倾向——学久了，
// 只有「像你以前收的」才排前面，新方向永远出不了头，这叫回音壁。
// 光靠调小分值治不了，因为问题不在幅度而在方向。
// 按赛道命中做闸门就干净了：赛道内的排序随你走，赛道外一条都进不来，
// 而「有没有新方向」由摄入侧决定，跟这一层无关。
func ApplyLearnt(r Result, title string, words []string) Result {
	if len(words) == 0 || r.Vetoed || r.NicheHit == 0 {
		return r
	}

	bonus := 0
	var hit []string
	for _, w := range words {
		if strings.Contains(title, w) {
			hit = append(hit, w)
			bonus += learnedPerWord
		}
	}
	if bonus == 0 {
		return r
	}
	if bonus > learnedCap {
		bonus = learnedCap
	}

	r.Score += bonus
	if r.Score > 100 {
		r.Score = 100
	}

	// 理由要说人话，而且必须带上具体是哪个词。
	// 只写「符合你的偏好」等于什么都没说，用户没法验证这个判断对不对。
	r.Reason += " · 你常收带「" + strings.Join(hit, "、") + "」的"
	return r
}

// Evaluate 是打一条内容的总入口：规则分 → 人设否决 → 偏好微调。
//
// 收成一个函数而不是让每个调用点自己串，是因为这四步的顺序本身就是结论：
// 否决必须在偏好之后生效（否决压过偏好）、偏好必须在规则之后（规则是基线）。
// 四个调用点各写一遍，迟早有一处把顺序写反，而那个 bug 不会报错，
// 只会让「我填的禁忌没生效」在某个奇怪的场景下复现。
func Evaluate(h source.Hotspot, niche string, now time.Time, p Prefs, learnt []string) Result {
	return ApplyLearnt(ApplyPrefs(Score(h, niche, now), h.Title, p), h.Title, learnt)
}

// reason 拼一句「为什么这条值得写」。
//
// 排序规则很重要：不能按得分率选，否则时效和热度永远排前面——那只是数据复述，
// 用户从列表里已经看得见。真正该说的是「跟你赛道有什么关系」。
func reason(fs []Factor) string {
	var parts []string
	for _, f := range fs {
		switch f.Name {
		case "赛道相关":
			if f.Points > 0 {
				parts = append(parts, f.Detail)
			}
		case "标题结构":
			if f.Points > 0 {
				parts = append(parts, f.Detail)
			}
		}
	}
	if len(parts) > 0 {
		return strings.Join(parts, " + ")
	}

	// 兜底：赛道和结构都没贡献，说明这条是噪音，只说明它热在哪。
	var heat, fresh string
	for _, f := range fs {
		switch f.Name {
		case "热度":
			heat = f.Detail
		case "时效":
			fresh = f.Detail
		}
	}
	if heat == "" {
		return "与当前赛道关联弱"
	}
	return "赛道关联弱，" + heat + "，" + fresh
}

func hasDigit(s string) bool {
	for _, r := range s {
		if r >= '0' && r <= '9' {
			return true
		}
	}
	return false
}

func human(n int64) string {
	switch {
	case n >= 10000:
		return fmt.Sprintf("%.1f万", float64(n)/10000)
	case n >= 1000:
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%d", n)
}

func ago(hours float64) string {
	switch {
	case hours < 1:
		return "1 小时内"
	case hours < 24:
		return fmt.Sprintf("%.0f 小时前", hours)
	default:
		return fmt.Sprintf("%.0f 天前", hours/24)
	}
}
