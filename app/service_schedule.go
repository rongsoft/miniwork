package main

import (
	"context"
	"log"
	"time"

	"rongsoft/miniwork/app/internal/schedule"
	"rongsoft/miniwork/app/internal/store"
)

// 定时刷新：每天到点自动抓一批新选题。
//
// 为什么值得做：这个产品的价值是「你每天早上打开就有新东西看」。
// 如果这需要用户自己点一下，那它就是「一个需要你操作的取数工具」，
// 而不是「每天自己长出选题的助手」。定时器是产品定位的一部分，不是附加功能。

// ScheduleView 是设置页上那一格。
type ScheduleView struct {
	Enabled bool   `json:"enabled"`
	Time    string `json:"time"` // "07:30"

	// 两个给人看的标签，前端不自己算日期。
	LastRunLabel string `json:"lastRunLabel"` // 「今天已刷新」/「还没跑过」
	NextLabel    string `json:"nextLabel"`    // 「明天 07:30」
}

// Schedule 返回当前定时设置。
func (s *TopicService) Schedule() (ScheduleView, error) {
	cfg, last := s.loadSchedule()
	now := time.Now()
	v := ScheduleView{
		Enabled:      cfg.Enabled,
		Time:         cfg.Clock(),
		LastRunLabel: lastRunLabel(last, now),
	}
	if cfg.Enabled {
		v.NextLabel = nextRunLabel(schedule.NextRun(now, cfg), now)
	}
	return v, nil
}

// SaveSchedule 保存定时设置。改完立刻生效，不需要重启——
// scheduler 每次 tick 都重新读配置，这是当初不用 AfterFunc 的原因之一。
func (s *TopicService) SaveSchedule(v ScheduleView) error {
	h, m, err := schedule.ParseClock(v.Time)
	if err != nil {
		return err
	}
	cfg := schedule.Config{Enabled: v.Enabled, Hour: h, Minute: m}.Normalize()

	if err := s.st.SetSetting(store.SettingScheduleEnabled, boolStr(cfg.Enabled)); err != nil {
		return err
	}
	return s.st.SetSetting(store.SettingScheduleTime, cfg.Clock())
}

// startScheduler 起后台定时器。main 里在 app 创建之后调用一次。
func (s *TopicService) startScheduler() {
	sch := schedule.New(schedule.TickInterval)
	sch.Load = s.loadSchedule
	sch.Run = s.runScheduled

	sch.OnRan = func(cfg schedule.Config) {
		log.Printf("定时刷新完成（每天 %s）", cfg.Clock())
		if s.app != nil {
			s.app.Event.Emit("schedule:ran", map[string]any{
				"at": time.Now().Format(time.RFC3339),
			})
		}
	}
	sch.OnErr = func(err error) {
		log.Printf("定时刷新失败: %v", err)
		if s.app != nil {
			s.app.Event.Emit("schedule:fail", map[string]any{"err": err.Error()})
		}
	}
	sch.OnSkip = func(why string) { log.Printf("定时刷新本次不跑：%s", why) }

	s.sched = sch
	sch.Start(context.Background())
}

// loadSchedule 每次现读，不缓存。读库比一次 tick 的成本低得多，
// 而缓存会引入「界面改了但钟还是老时间」这种最难查的问题。
func (s *TopicService) loadSchedule() (schedule.Config, string) {
	h, m, _ := schedule.ParseClock(s.st.GetSetting(store.SettingScheduleTime))
	cfg := schedule.Config{
		Enabled: s.st.GetSetting(store.SettingScheduleEnabled) == "1",
		Hour:    h,
		Minute:  m,
	}.Normalize()
	return cfg, s.st.GetSetting(store.SettingScheduleLastRun)
}

// runScheduled 是定时器真正要执行的动作。
// 返回 ran=false 表示正忙被挡下了，让 scheduler 退避几分钟再来。
func (s *TopicService) runScheduled(schedule.Config) (bool, error) {
	if !s.begin() {
		return false, nil
	}
	defer s.end()

	if _, err := s.runLocked(s.currentNiche(), ""); err != nil {
		return false, err
	}
	return true, nil
}

// currentNiche 是后台跑用的是哪个赛道：以人设为准。
//
// 用赛道而不是「用户上次在界面上选的那个」：定时器跑的时候界面可能还没打开，
// 拿不到界面状态；而人设里的赛道是用户明确写下的长期方向，比一次性的下拉选择更稳。
func (s *TopicService) currentNiche() string {
	p, err := s.st.Profile()
	if err != nil || p.Niche == "" {
		return "职场"
	}
	return p.Niche
}

func boolStr(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// lastRunLabel 说清「最近的自动数据是什么时候的」。
//
// 只精确到天：SettingScheduleLastRun 存的就是日期。要说「今天 07:31」
// 得再存一份时刻，而这对用户没有任何用处——他关心的是今天有没有新的。
func lastRunLabel(last string, now time.Time) string {
	if last == "" {
		return "还没跑过"
	}
	today := now.Format(store.DateLayout)
	switch last {
	case today:
		return "今天已刷新"
	case now.AddDate(0, 0, -1).Format(store.DateLayout):
		return "昨天刷新过"
	default:
		if len(last) >= 10 {
			return last[5:] + " 刷新过"
		}
		return last
	}
}

func nextRunLabel(at, now time.Time) string {
	hm := at.Format("15:04")
	today := now.Format(store.DateLayout)
	switch at.Format(store.DateLayout) {
	case today:
		return "今天 " + hm
	case now.AddDate(0, 0, 1).Format(store.DateLayout):
		return "明天 " + hm
	default:
		return at.Format(store.DateLayout)[5:] + " " + hm
	}
}
