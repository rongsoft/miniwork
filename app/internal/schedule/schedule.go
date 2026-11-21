// Package schedule 负责「每天到点自动刷一次」。
//
// 只做两件事：判断此刻该不该跑（纯函数，可测），以及每分钟问一次自己。
//
// 为什么用 ticker 每分钟问一次，而不是 AfterFunc 一把算到明天：
//   - 用户在界面上改时间，要立刻生效，不能等他重启；
//   - 笔记本合盖休眠，AfterFunc 醒来后不准点，ticker 醒来照样问一次「现在几点、该不该跑」；
//   - 时区/系统时钟被 NTP 校正，算好的绝对时刻就废了。
//
// 这三种情况用「每分钟重新判断一次」全部自动正确，代价是每分钟一次空转。
package schedule

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Config 是用户能改的两件事：开不开、几点。
type Config struct {
	Enabled bool
	Hour    int
	Minute  int
}

const (
	DefaultHour   = 8
	DefaultMinute = 30

	// GraceWindow 是错过时间点之后还愿意补跑多久。
	//
	// 没有它：用户设 07:30，晚上十点才开机，程序立刻开始抓数——他要的是
	// 明天的选题，这时候抓回来的是「今天剩下的热点」，还白扣一次配额。
	// 有了它：超过 6 小时就安静地等明天，不补。
	GraceWindow = 6 * time.Hour

	// TickInterval 是检查间隔。一分钟以内足够，再密没有意义。
	TickInterval = 60 * time.Second

	// busyRetry 是「程序正忙」时的退避。用户手动点刷新时定时器撞上了，
	// 不该每分钟都去抢一次锁，等几分钟再来。
	busyRetry = 3 * time.Minute
)

// Normalize 把越界的时间掰回合法范围。用户从界面传来的值不可全信。
func (c Config) Normalize() Config {
	if c.Hour < 0 || c.Hour > 23 {
		c.Hour = DefaultHour
	}
	if c.Minute < 0 || c.Minute > 59 {
		c.Minute = DefaultMinute
	}
	return c
}

// Clock 返回 "07:30" 这种形态，给界面和日志用。
func (c Config) Clock() string { return fmt.Sprintf("%02d:%02d", c.Hour, c.Minute) }

// ParseClock 解析 "7:30" / "07:30"，容错到用户手输的常见形态。
// 空串返回默认值而不是错误：这一项没填不算错。
func ParseClock(s string) (h, m int, err error) {
	s = trimSpace(s)
	if s == "" {
		return DefaultHour, DefaultMinute, nil
	}
	n, err := fmt.Sscanf(s, "%d:%d", &h, &m)
	if err != nil || n != 2 {
		return 0, 0, fmt.Errorf("时间格式应为 07:30，收到 %q", s)
	}
	if h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, 0, fmt.Errorf("时间超出范围: %s", s)
	}
	return h, m, nil
}

// Due 判断此刻是否该自动跑一次，第二个返回值是给日志和界面用的原因。
//
// lastRun 是「上一次刷新的日期」yyyy-mm-dd（含手动刷新）。它必须是日期而不是
// 时刻：判的是「今天有没有过新数据」，不是「上次跑是几点几分」。
func Due(now time.Time, c Config, lastRun string) (bool, string) {
	if !c.Enabled {
		return false, ""
	}
	today := now.Format("2006-01-02")

	// 用 >= 而不是 ==：系统时钟回拨会让 lastRun 落到未来，那时既不该补跑，
	// 也不该因为「等于判断失败」而立刻重跑一次。
	if lastRun >= today {
		return false, ""
	}

	at := time.Date(now.Year(), now.Month(), now.Day(), c.Hour, c.Minute, 0, 0, now.Location())
	if now.Before(at) {
		return false, ""
	}
	if now.Sub(at) > GraceWindow {
		return false, fmt.Sprintf("已过 %s 超过 %d 小时，今天不补跑",
			c.Clock(), int(GraceWindow.Hours()))
	}
	return true, fmt.Sprintf("到了每天 %s，今天还没刷新过", c.Clock())
}

// NextRun 返回下一次预定时刻，给界面显示「下次 明天 07:30」。
func NextRun(now time.Time, c Config) time.Time {
	at := time.Date(now.Year(), now.Month(), now.Day(), c.Hour, c.Minute, 0, 0, now.Location())
	if !now.Before(at) {
		at = at.AddDate(0, 0, 1)
	}
	return at
}

// Scheduler 是那台每分钟响一次的钟。
//
// 所有外部依赖都做成字段注入，Start 里不碰数据库也不碰界面——
// 这样测试可以直接调 Tick() 而不用等一分钟。
type Scheduler struct {
	every time.Duration

	// Load 每次 tick 都重新读配置，用户改完界面立即生效。
	Load func() (Config, string)
	// Run 执行一次刷新。返回 ran=false 表示因为正忙被挡下了，需要退避重试。
	Run func(Config) (ran bool, err error)
	// OnRan / OnErr / OnSkip 是给日志和界面事件的出口，都可为空。
	OnRan  func(Config)
	OnErr  func(error)
	OnSkip func(string)

	now       func() time.Time
	skipUntil time.Time
}

func New(every time.Duration) *Scheduler {
	if every <= 0 {
		every = TickInterval
	}
	return &Scheduler{every: every, now: time.Now}
}

// Start 起一个后台 goroutine，ctx 取消即退出。不阻塞调用方。
func (s *Scheduler) Start(ctx context.Context) {
	go func() {
		// 先等界面起来，再判断一次。
		//
		// 用户很可能是在过了时间点之后才打开程序（比如 08:30 的设定，
		// 中午才开机）。这时候等满一分钟才有数据没道理，所以启动就判一次；
		// 但要等三秒——首启动时前端也在决定要不要自己刷，两边同时冲上去
		// 只会撞运行锁，谁也没跑成。
		select {
		case <-ctx.Done():
			return
		case <-time.After(3 * time.Second):
		}
		s.Tick()

		t := time.NewTicker(s.every)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.Tick()
			}
		}
	}()
}

// Tick 检查一次。导出是为了测试能直接驱动，不用真的等。
func (s *Scheduler) Tick() {
	if s.Load == nil || s.Run == nil {
		return
	}
	now := s.now()
	if now.Before(s.skipUntil) {
		return
	}

	cfg, lastRun := s.Load()
	due, why := Due(now, cfg, lastRun)
	if !due {
		if why != "" && s.OnSkip != nil {
			s.OnSkip(why)
		}
		return
	}

	ran, err := s.Run(cfg)
	if err != nil {
		if s.OnErr != nil {
			s.OnErr(err)
		}
		// 报错不立刻重试：数据源挂了时每分钟打一次只会刷满日志，
		// 下一轮 tick 会自然重来。
		s.skipUntil = now.Add(busyRetry)
		return
	}
	if !ran {
		s.skipUntil = now.Add(busyRetry)
		return
	}
	if s.OnRan != nil {
		s.OnRan(cfg)
	}
}

// trimSpace 只去首尾空白。
func trimSpace(s string) string { return strings.TrimSpace(s) }
