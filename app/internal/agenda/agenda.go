// Package agenda 回答一个产品问题：今天该干什么。
//
// 它不是「所有待办的列表」，而是按「现在最该动哪个」排过序的一小队任务。
// 排序规则就是这个包的全部价值：
//
//	逾期未发 > 今天要发 > 卡住没动 > 还没决定
//
// 逾期排第一，是因为它伤的是承诺：用户在日历上写了「14 号发」，
// 今天 15 号还没发，这件事比「新来了一批选题没看」严重得多。
package agenda

import (
	"fmt"
	"time"

	"rongsoft/miniwork/app/internal/store"
)

// 四类任务。界面按 Kind 决定图标和跳转动作，不靠文案猜。
const (
	KindOverdue = "overdue" // 排期已过、还没发
	KindDue     = "due"     // 今天要发
	KindStuck   = "stuck"   // 停在某个状态太久
	KindDecide  = "decide"  // 待定里的高分，还没处理
)

// Item 是今天该做的一件事。
type Item struct {
	Kind         string `json:"kind"`
	TopicID      string `json:"topicId"`
	Title        string `json:"title"`
	Platform     string `json:"platform"`
	PlatformName string `json:"platformName"`
	Status       string `json:"status"`
	StatusLabel  string `json:"statusLabel"`
	Score        int    `json:"score"`
	PlanDate     string `json:"planDate"`
	Days         int    `json:"days"` // 逾期/卡住天数
	Hint         string `json:"hint"` // 一句为什么它出现在这里
}

// Day 是今天这一屏。
type Day struct {
	Date  string `json:"date"`
	Items []Item `json:"items"`

	Overdue int `json:"overdue"`
	Due     int `json:"due"`
	Stuck   int `json:"stuck"`
	Decide  int `json:"decide"`

	Total    int    `json:"total"`
	Headline string `json:"headline"`
}

// Input 是从库里取出、已经分好类的原料。
// 分开传是为了让 Build 保持纯函数——分类靠 SQL，排序和文案在这里，各自可测。
type Input struct {
	Now     time.Time
	Due     []store.Topic // plan_date = 今天 且已排期
	Overdue []store.Topic // plan_date < 今天 且已排期
	Stuck   []store.Aging // 停在 writing / picked 太久
	Decide  []store.Topic // 待定里分数够高
}

const (
	// DecideMinScore 是「值得现在决定」的门槛。
	// 低于它的一堆 50 分选题不该占用户的注意力。
	DecideMinScore = 70
	// DecideLimit 是待办里最多列几条待决定。
	// 列十条等于没列，用户会全部划过去。
	DecideLimit = 3
)

// Build 把原料排成一队。
func Build(in Input) Day {
	d := Day{Date: in.Now.Format(store.DateLayout)}

	for _, t := range in.Overdue {
		days := store.DayGap(mustDate(t.PlanDate), in.Now)
		d.Overdue++
		d.Items = append(d.Items, Item{
			Kind:        KindOverdue,
			TopicID:     t.ID,
			Title:       t.Title,
			Platform:    t.Platform,
			Status:      string(t.Status),
			StatusLabel: t.Status.Label(),
			Score:       t.Score,
			PlanDate:    t.PlanDate,
			Days:        days,
			Hint:        overdueHint(t.PlanDate, days),
		})
	}

	for _, t := range in.Due {
		d.Due++
		d.Items = append(d.Items, Item{
			Kind:        KindDue,
			TopicID:     t.ID,
			Title:       t.Title,
			Platform:    t.Platform,
			Status:      string(t.Status),
			StatusLabel: t.Status.Label(),
			Score:       t.Score,
			PlanDate:    t.PlanDate,
			Hint:        "排的今天，写完就能发",
		})
	}

	for _, a := range in.Stuck {
		d.Stuck++
		d.Items = append(d.Items, Item{
			Kind:        KindStuck,
			TopicID:     a.ID,
			Title:       a.Title,
			Platform:    a.Platform,
			Status:      string(a.Status),
			StatusLabel: a.Status.Label(),
			Score:       a.Score,
			PlanDate:    a.PlanDate,
			Days:        a.Days,
			Hint:        fmt.Sprintf("%s停了 %d 天", a.Status.Label(), a.Days),
		})
	}

	decided := 0
	for _, t := range in.Decide {
		if decided >= DecideLimit {
			break
		}
		// 平台名在 service 层补（那边已经有 format 依赖），这里留空不影响排序。
		d.Decide++
		decided++
		d.Items = append(d.Items, Item{
			Kind:        KindDecide,
			TopicID:     t.ID,
			Title:       t.Title,
			Platform:    t.Platform,
			Status:      string(t.Status),
			StatusLabel: t.Status.Label(),
			Score:       t.Score,
			Hint:        fmt.Sprintf("%d 分，等着你决定", t.Score),
		})
	}

	d.Total = len(d.Items)
	d.Headline = headline(d)
	return d
}

// overdueHint 说清「本来该哪天发、拖了几天」。
// 「已逾期」三个字没有任何信息量，用户点进去才知道拖了多久，等于白说。
func overdueHint(planDate string, days int) string {
	if days <= 0 {
		return "排的今天，还没发"
	}
	return fmt.Sprintf("排的 %s，拖了 %d 天", shortDate(planDate), days)
}

// headline 是这一屏最上面那句话。全空时不要写「暂无待办」——
// 那像是在催人，而其实是他今天可以休息。
func headline(d Day) string {
	switch {
	case d.Total == 0:
		return "今天没有非做不可的事"
	case d.Overdue > 0 && d.Due > 0:
		return fmt.Sprintf("%d 条过了排期没发，%d 条今天要发", d.Overdue, d.Due)
	case d.Overdue > 0:
		return fmt.Sprintf("%d 条过了排期还没发", d.Overdue)
	case d.Due > 0:
		return fmt.Sprintf("今天要发 %d 条", d.Due)
	case d.Stuck > 0:
		return fmt.Sprintf("%d 条停着没动", d.Stuck)
	default:
		return fmt.Sprintf("%d 条高分选题等你决定", d.Decide)
	}
}

// shortDate 把 2026-11-14 缩成 11-14。日历和待办里都不需要年份。
func shortDate(s string) string {
	if len(s) < 10 {
		return s
	}
	return s[5:10]
}

func mustDate(s string) time.Time {
	t, err := time.Parse(store.DateLayout, s)
	if err != nil {
		return time.Time{}
	}
	return t
}
