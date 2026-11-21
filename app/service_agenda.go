package main

import (
	"time"

	"rongsoft/miniwork/app/internal/agenda"
	"rongsoft/miniwork/app/internal/format"
	"rongsoft/miniwork/app/internal/store"
	"rongsoft/miniwork/app/internal/topic"
)

// 今日待办与日历。
//
// 这两块看起来是两个功能，其实是同一件事的两端：日历是「我打算怎么安排」，
// 待办是「今天到期了哪些」。用的都是 topic.plan_date 一个字段。

// 卡住多久才算「该提一句」。
//
// 写作中给 2 天：写完一篇通常就在这一两天内，第三天还挂着基本是卡了。
// 已收下给 7 天：留着慢慢写的选题很正常，一周还没动才叫真的没动。
const (
	stuckWritingDays = 2
	stuckPickedDays  = 7
)

// Today 返回「今天该干什么」。
func (s *TopicService) Today() (agenda.Day, error) {
	now := time.Now()
	today := now.Format(store.DateLayout)

	plan, err := s.st.PlanOn(today)
	if err != nil {
		return agenda.Day{}, err
	}
	// PlanOn 返回当天所有状态的：已发的也在里面（日历要显示），
	// 但「今天要发」只算还没发的。
	due := make([]store.Topic, 0, len(plan))
	for _, t := range plan {
		if t.Status == topic.Scheduled {
			due = append(due, t)
		}
	}

	overdue, err := s.st.Overdue(today)
	if err != nil {
		return agenda.Day{}, err
	}

	stuck, err := s.stuckTopics(now)
	if err != nil {
		return agenda.Day{}, err
	}

	drafts, err := s.st.TopDraft(20)
	if err != nil {
		return agenda.Day{}, err
	}
	decide := make([]store.Topic, 0, agenda.DecideLimit)
	for _, t := range drafts {
		if t.Score >= agenda.DecideMinScore {
			decide = append(decide, t)
		}
	}

	day := agenda.Build(agenda.Input{
		Now: now, Due: due, Overdue: overdue, Stuck: stuck, Decide: decide,
	})
	for i := range day.Items {
		day.Items[i].PlatformName = format.Platform(day.Items[i].Platform)
	}
	return day, nil
}

// stuckTopics 合并两类卡住：写得停住的、收了不动的。
func (s *TopicService) stuckTopics(now time.Time) ([]store.Aging, error) {
	w, err := s.st.StuckIn(topic.Writing, now.AddDate(0, 0, -stuckWritingDays))
	if err != nil {
		return nil, err
	}
	p, err := s.st.StuckIn(topic.Picked, now.AddDate(0, 0, -stuckPickedDays))
	if err != nil {
		return nil, err
	}
	// 不要用 append(w, p...) 后直接返回：p 为空时会退化成 w 本身，
	// 调用方再往里加东西就会踩到库里的切片。这里显式新建。
	out := make([]store.Aging, 0, len(w)+len(p))
	out = append(out, w...)
	out = append(out, p...)
	return out, nil
}

// CalendarCell 是日历上的一格。空的 Date 表示月初/月末的占位。
type CalendarCell struct {
	Date    string      `json:"date"`
	Day     int         `json:"day"`
	Today   bool        `json:"today"`
	Weekend bool        `json:"weekend"`
	Items   []TopicView `json:"items"`
}

// CalendarView 是日历一屏。
type CalendarView struct {
	Month   string         `json:"month"`   // 2026-11
	Title   string         `json:"title"`   // 2026 年 11 月
	Cells   []CalendarCell `json:"cells"`   // 长度是 7 的倍数
	Pending []TopicView    `json:"pending"` // 收了但还没排期的，可以拖进格子

	Planned   int `json:"planned"`   // 本月排了几条
	Published int `json:"published"` // 里面发出去几条
	Overdue   int `json:"overdue"`   // 里面过期没发几条
}

// Calendar 返回某个月的日历。month 传空表示本月。
func (s *TopicService) Calendar(month string) (CalendarView, error) {
	now := time.Now()
	first := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	if month != "" {
		if t, err := time.Parse("2006-01", month); err == nil {
			first = time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, now.Location())
		}
	}
	last := first.AddDate(0, 1, -1)
	today := now.Format(store.DateLayout)

	list, err := s.st.PlanBetween(first.Format(store.DateLayout), last.Format(store.DateLayout))
	if err != nil {
		return CalendarView{}, err
	}

	byDay := map[string][]TopicView{}
	v := CalendarView{
		Month:   first.Format("2006-01"),
		Title:   first.Format("2006 年 1 月"),
		Planned: len(list),
	}
	for _, t := range list {
		byDay[t.PlanDate] = append(byDay[t.PlanDate], toView(t))
		switch t.Status {
		case topic.Published:
			v.Published++
		case topic.Scheduled:
			if t.PlanDate < today {
				v.Overdue++
			}
		}
	}

	// 前面补空位：周一开头还是周日开头，取决于本地习惯，这里用周日开头
	// （Weekday() 的 0 就是周日，不用换算）。
	lead := int(first.Weekday())
	cells := make([]CalendarCell, 0, 42)
	for i := 0; i < lead; i++ {
		cells = append(cells, CalendarCell{})
	}
	for d := 1; d <= last.Day(); d++ {
		date := time.Date(first.Year(), first.Month(), d, 0, 0, 0, 0, first.Location())
		wd := date.Weekday()
		cells = append(cells, CalendarCell{
			Date:    date.Format(store.DateLayout),
			Day:     d,
			Today:   date.Format(store.DateLayout) == today,
			Weekend: wd == time.Saturday || wd == time.Sunday,
			Items:   byDay[date.Format(store.DateLayout)],
		})
	}
	for len(cells)%7 != 0 {
		cells = append(cells, CalendarCell{})
	}
	v.Cells = cells
	v.Pending = s.pendingPlan()

	return v, nil
}

// pendingPlan 取「在手上但还没排期」的选题，日历右侧那栏用。
//
// 只取 picked 和 writing：draft 还没决定，不该出现在排期候选里，
// 否则用户会开始给没看过的选题安排日子。
func (s *TopicService) pendingPlan() []TopicView {
	var out []TopicView
	for _, st := range []topic.Status{topic.Writing, topic.Picked} {
		list, err := s.st.ListByStatus(st, 30)
		if err != nil {
			continue // 这一栏缺了不影响主体，日历照样能看
		}
		out = append(out, toViews(list)...)
	}
	return out
}

// Plan 把选题排到某天，date 传空表示取消排期。
func (s *TopicService) Plan(id, date string) error {
	return s.st.Schedule(id, date)
}
