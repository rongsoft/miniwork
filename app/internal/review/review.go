// Package review 是复盘：回头看自己做过什么决定、这些决定准不准。
//
// 它诚实地只管一件事——「你的判断」，不管「效果」。
// 播放量、赞、涨粉这些数据这个程序拿不到，硬编进去就是假数。
// 所以这里所有的数字都能从 topic 和 topic_event 里算出来，一个都不用用户填。
//
// 结论用规则拼，不调模型：复盘是每天都可能会点开的东西，
// 每次点开都花一次钱、等三秒，用户第三次就不点了。
package review

import (
	"fmt"
	"sort"
	"time"

	"rongsoft/miniwork/app/internal/store"
	"rongsoft/miniwork/app/internal/topic"
)

// Windows 是界面上能选的三个窗口。
var Windows = []int{7, 30, 90}

// Window 是这次复盘的时间范围。
type Window struct {
	Days  int    `json:"days"`
	From  string `json:"from"`
	To    string `json:"to"`
	Label string `json:"label"` // 「最近 7 天」
}

// Step 是漏斗的一级。
type Step struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Count int    `json:"count"`
	Pct   int    `json:"pct"` // 相对「产出」的百分比，整数
}

// AICheck 是「模型和你合不合拍」。
//
// 这是整个复盘里唯一能验证「AI 值不值得开」的数据：模型给高分的你收了没有？
// 收得多说明它在替你筛；收得少说明它还没懂你，该去把人设写清楚。
type AICheck struct {
	Available  bool   `json:"available"`
	HighTotal  int    `json:"highTotal"`  // 模型给 >= AIGoodFit 分
	HighKept   int    `json:"highKept"`   // 里面的被你收下或更靠后
	LowTotal   int    `json:"lowTotal"`   // 模型给 < AIBadFit 分
	LowDropped int    `json:"lowDropped"` // 里面的被你丢掉
	Verdict    string `json:"verdict"`
}

// DayPoint 是趋势图上的一天。
type DayPoint struct {
	Day       string `json:"day"` // 11-15
	New       int    `json:"new"`
	Picked    int    `json:"picked"`
	Published int    `json:"published"`
}

// Regret 是一条「你丢掉了但当时分数很高」的选题。
type Regret struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Score int    `json:"score"`
	Days  int    `json:"days"`
	// Platform 只在包内传递，界面要的是 PlatformName；
	// 转换放在 service 层做，这里不引入 format 依赖。
	Platform     string `json:"-"`
	PlatformName string `json:"platformName"`
}

// StuckItem 是一条停着没动的选题。
type StuckItem struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	StatusLabel string `json:"statusLabel"`
	Days        int    `json:"days"`
}

// Review 是界面需要的全部数据。
type Review struct {
	Window Window `json:"window"`

	Generated int `json:"generated"` // 窗口内产出选题
	Picked    int `json:"picked"`    // 收下（含之后各阶段）
	Dropped   int `json:"dropped"`
	Pending   int `json:"pending"` // 还没决定
	Published int `json:"published"`

	PickRate int `json:"pickRate"` // 收下 / 产出
	ShipRate int `json:"shipRate"` // 发布 / 收下

	Funnel []Step     `json:"funnel"`
	AI     AICheck    `json:"ai"`
	Trend  []DayPoint `json:"trend"`

	Regrets []Regret    `json:"regrets"`
	Stuck   []StuckItem `json:"stuck"`

	Headline string   `json:"headline"`
	Notes    []string `json:"notes"`
}

const (
	// AIGoodFit 是「模型觉得这条很像你的号」的分数线。
	AIGoodFit = 70
	// AIBadFit 是「模型觉得这条不像」的线。
	AIBadFit = 50
	// RegretMinScore 是「值得回看的丢弃」门槛。低于它的丢掉不叫遗憾，叫正常筛选。
	RegretMinScore = 75
	// RegretLimit 是最多回看几条。
	RegretLimit = 4
)

// Input 是取好的原料，Build 保持纯函数。
type Input struct {
	Now      time.Time
	Days     int
	Counts   map[string]int // 窗口内产出的选题「现在」各状态分布
	Created  int            // 窗口内产出总数
	NewByDay map[string]int // 每天新增几条（yyyy-mm-dd → 条数）
	AIPairs  []store.AIPair
	Events   []store.Event
	Regrets  []store.Topic
	Stuck    []store.Aging
}

// Build 算出这次复盘。
func Build(in Input) Review {
	days := in.Days
	if days <= 0 {
		days = 7
	}
	from := in.Now.AddDate(0, 0, -days+1)

	r := Review{
		Window: Window{
			Days:  days,
			From:  from.Format(store.DateLayout),
			To:    in.Now.Format(store.DateLayout),
			Label: fmt.Sprintf("最近 %d 天", days),
		},
		Generated: in.Created,
		Pending:   in.Counts[string(topic.Draft)],
		Dropped:   in.Counts[string(topic.Dropped)],
		Published: in.Counts[string(topic.Published)],
	}
	// 收下 = 除了「待定」和「已丢」之外的所有状态。
	// 这样算而不是数事件，是因为要看「这批选题现在还有多少在手里」，
	// 而不是「这段时间点了几下收下」——后者会被来回改状态灌水。
	for k, n := range in.Counts {
		switch topic.Status(k) {
		case topic.Draft, topic.Dropped:
		default:
			r.Picked += n
		}
	}

	if r.Generated > 0 {
		r.PickRate = pct(r.Picked, r.Generated)
	}
	if r.Picked > 0 {
		r.ShipRate = pct(r.Published, r.Picked)
	}

	r.Funnel = []Step{
		{Key: "generated", Label: "产出选题", Count: r.Generated, Pct: 100},
		{Key: "picked", Label: "你收下", Count: r.Picked, Pct: pct(r.Picked, r.Generated)},
		{Key: "scheduled", Label: "排上期", Count: r.Picked - in.Counts[string(topic.Picked)] - in.Counts[string(topic.Writing)], Pct: 0},
		{Key: "published", Label: "发出去了", Count: r.Published, Pct: pct(r.Published, r.Generated)},
	}
	// 漏斗百分比一律相对「产出」：四级各自除以收下数会让第一级变成 100%、
	// 后面几级看不出绝对量的衰减，反而更难看懂。
	for i := range r.Funnel {
		r.Funnel[i].Pct = pct(r.Funnel[i].Count, r.Generated)
	}
	if r.Funnel[2].Count < 0 {
		r.Funnel[2].Count = 0
	}

	r.AI = buildAI(in.AIPairs)
	r.Trend = buildTrend(in.Now, days, in.Events, in.NewByDay)
	r.Regrets = buildRegrets(in.Now, in.Regrets, in.Events)
	r.Stuck = buildStuck(in.Stuck)

	r.Notes = notes(r)
	r.Headline = headline(r)
	return r
}

// buildAI 算模型校准。
func buildAI(pairs []store.AIPair) AICheck {
	var c AICheck
	for _, p := range pairs {
		if p.Fit >= AIGoodFit {
			c.HighTotal++
			// 「收下」包括之后所有状态：收下之后写没写、发没发，都说明它被认可了。
			if p.Status != string(topic.Draft) && p.Status != string(topic.Dropped) {
				c.HighKept++
			}
		}
		if p.Fit < AIBadFit {
			c.LowTotal++
			if p.Status == string(topic.Dropped) {
				c.LowDropped++
			}
		}
	}
	c.Available = c.HighTotal+c.LowTotal > 0
	if !c.Available {
		c.Verdict = "这批选题没走过模型，暂时看不出它准不准"
		return c
	}
	if c.HighTotal < 3 {
		c.Verdict = fmt.Sprintf("模型只给过 %d 条高分，样本太少，再攒几天", c.HighTotal)
		return c
	}

	rate := pct(c.HighKept, c.HighTotal)
	switch {
	case rate >= 80:
		c.Verdict = fmt.Sprintf("它挑出的 %d 条高分，你收了 %d 条——它开始懂你了", c.HighTotal, c.HighKept)
	case rate >= 40:
		c.Verdict = fmt.Sprintf("它挑的 %d 条高分你收了 %d 条，方向对，标准还不完全一样", c.HighTotal, c.HighKept)
	default:
		c.Verdict = fmt.Sprintf("它挑的 %d 条高分你只收了 %d 条——多半是它还不够了解你，去把人设写细点", c.HighTotal, c.HighKept)
	}
	return c
}

// buildTrend 按自然日聚合，再按窗口长度合并成固定的十几个点。
//
// 为什么要合并：90 天窗口画 90 根柱子，每根不到两像素宽，什么也看不出来。
// 窗口越长，一格代表的时间越粗——7 天看每天、一个月看三天、一季度看每周。
// 合并从最新一天往前做，保证最后一格永远是完整的，不会出现「今天只有半天」。
func buildTrend(now time.Time, days int, events []store.Event, newByDay map[string]int) []DayPoint {
	step := 1
	switch {
	case days > 45:
		step = 7
	case days > 14:
		step = 3
	}

	daily := make([]DayPoint, days)
	idx := make(map[string]int, days)
	for i := 0; i < days; i++ {
		d := now.AddDate(0, 0, -(days - 1 - i))
		key := d.Format(store.DateLayout)
		daily[i] = DayPoint{Day: key[5:], New: newByDay[key]}
		idx[key] = i
	}

	// 一条选题可能被丢掉再捡回来再收下，那样「收下」会被数两次。
	// 用 seen 保证每条最多贡献一次，趋势图才不是虚的。
	seen := map[string]bool{}
	for _, e := range events {
		i, ok := idx[e.At.Format(store.DateLayout)]
		if !ok {
			continue
		}
		switch topic.Status(e.To) {
		case topic.Picked:
			if seen[e.TopicID] {
				continue
			}
			seen[e.TopicID] = true
			daily[i].Picked++
		case topic.Published:
			daily[i].Published++
		}
	}

	out := make([]DayPoint, 0, days/step+1)
	for i := len(daily) - 1; i >= 0; i -= step {
		lo := i - step + 1
		if lo < 0 {
			lo = 0
		}
		sum := DayPoint{Day: daily[i].Day}
		for j := lo; j <= i; j++ {
			sum.New += daily[j].New
			sum.Picked += daily[j].Picked
			sum.Published += daily[j].Published
		}
		out = append([]DayPoint{sum}, out...)
	}
	return out
}

// buildRegrets 把丢掉的高分选题翻出来，并标出「丢了几天」。
//
// 天数取自 dropped 事件而不是 created_at：用户关心的是「这条我扔了多久之后
// 回头看它还是不是好选题」，扔之前放了几天无关。
func buildRegrets(now time.Time, list []store.Topic, events []store.Event) []Regret {
	droppedAt := map[string]time.Time{}
	for _, e := range events {
		if topic.Status(e.To) == topic.Dropped {
			// 取最后一次丢弃的时间：捡回来又丢，算最近这次。
			if t, ok := droppedAt[e.TopicID]; !ok || e.At.After(t) {
				droppedAt[e.TopicID] = e.At
			}
		}
	}
	out := make([]Regret, 0, len(list))
	for _, t := range list {
		out = append(out, Regret{
			ID:       t.ID,
			Title:    t.Title,
			Score:    t.Score,
			Days:     store.DayGap(droppedAt[t.ID], now),
			Platform: t.Platform,
		})
	}
	return out
}

func buildStuck(list []store.Aging) []StuckItem {
	out := make([]StuckItem, 0, len(list))
	for _, a := range list {
		if a.Days < 1 {
			continue
		}
		out = append(out, StuckItem{
			ID:          a.ID,
			Title:       a.Title,
			StatusLabel: a.Status.Label(),
			Days:        a.Days,
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Days > out[j].Days })
	return out
}

// notes 是几条具体判断。空话（「继续保持」「加油」）一句都不写——
// 用户看复盘是为了找出问题，不是为了被夸。
func notes(r Review) []string {
	var out []string

	if r.Generated == 0 {
		return []string{"这个窗口里还没有产出过选题。先回今日灵感刷一次。"}
	}

	// 1. 收下但不发，是创作者最常见也最痛的问题。
	if r.Picked > 0 && r.Published == 0 {
		out = append(out, fmt.Sprintf("收了 %d 条，一条都没发出去。问题不在选题，在写。", r.Picked))
	} else if r.Picked >= 5 && r.ShipRate < 30 {
		out = append(out, fmt.Sprintf("收下到发布只有 %d%%。收得比写得快，后面只会越堆越多。", r.ShipRate))
	}

	// 2. 只丢不收，说明标准定得比产能高，或者记忆里的禁忌写得太狠。
	if r.Picked == 0 && r.Dropped > 0 {
		out = append(out, fmt.Sprintf("看了 %d 条，一条都没留。要么放宽「不碰的话题」，要么换个方向看看。", r.Dropped))
	}

	// 3. 卡点比数字更有行动价值。
	if len(r.Stuck) > 0 {
		s := r.Stuck[0]
		out = append(out, fmt.Sprintf("《%s》在「%s」停了 %d 天，是这批里最久的一条。", clip(s.Title, 18), s.StatusLabel, s.Days))
	}

	// 4. 遗憾清单要克制：只在真的有高分被丢时才提。
	if len(r.Regrets) > 0 {
		out = append(out, fmt.Sprintf("丢掉了 %d 条 %d 分以上的选题，往下翻能看见是哪几条。", len(r.Regrets), RegretMinScore))
	}

	return out
}

// headline 是一句话总结，按「最该被看见的问题」排优先级。
func headline(r Review) string {
	switch {
	case r.Generated == 0:
		return "这段时间还没有数据"
	case r.Picked > 0 && r.Published == 0:
		return "收了没发"
	case r.Picked == 0 && r.Dropped > 0:
		return "只丢不收"
	case r.Published > 0:
		return fmt.Sprintf("发出 %d 条", r.Published)
	default:
		return fmt.Sprintf("%d 条等你决定", r.Pending)
	}
}

func pct(a, b int) int {
	if b <= 0 {
		return 0
	}
	return int(float64(a)*100/float64(b) + 0.5)
}

// clip 按字数截断，中文按字算而不是按字节，否则会在半个字上切出乱码。
func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
