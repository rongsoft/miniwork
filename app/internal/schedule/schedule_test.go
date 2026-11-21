package schedule

import (
	"testing"
	"time"
)

func d(y int, m time.Month, day, hh, mm int) time.Time {
	return time.Date(y, m, day, hh, mm, 0, 0, time.Local)
}

func TestDueDisabled(t *testing.T) {
	now := d(2026, 11, 15, 9, 0)
	ok, _ := Due(now, Config{Enabled: false, Hour: 7, Minute: 30}, "")
	if ok {
		t.Fatal("关掉的定时器不该跑")
	}
}

func TestDueNotYet(t *testing.T) {
	now := d(2026, 11, 15, 7, 29)
	ok, _ := Due(now, Config{Enabled: true, Hour: 7, Minute: 30}, "")
	if ok {
		t.Fatal("还差一分钟，不该跑")
	}
}

func TestDueOnTheMinute(t *testing.T) {
	now := d(2026, 11, 15, 7, 30)
	ok, why := Due(now, Config{Enabled: true, Hour: 7, Minute: 30}, "")
	if !ok {
		t.Fatalf("到点该跑，实际没跑（why=%q）", why)
	}
}

func TestDueAlreadyRanToday(t *testing.T) {
	now := d(2026, 11, 15, 9, 0)
	ok, _ := Due(now, Config{Enabled: true, Hour: 7, Minute: 30}, "2026-11-15")
	if ok {
		t.Fatal("今天已经跑过，不该再跑")
	}
}

func TestDueRanYesterday(t *testing.T) {
	now := d(2026, 11, 15, 9, 0)
	ok, _ := Due(now, Config{Enabled: true, Hour: 7, Minute: 30}, "2026-11-14")
	if !ok {
		t.Fatal("昨天跑过、今天到点，该跑")
	}
}

// 这是宽限窗存在的理由：晚上十点才开机，不该立刻抓一次「今天剩下的热点」。
func TestDueBeyondGrace(t *testing.T) {
	now := d(2026, 11, 15, 22, 0)
	ok, why := Due(now, Config{Enabled: true, Hour: 7, Minute: 30}, "")
	if ok {
		t.Fatalf("过了 14 小时还不该补跑（why=%q）", why)
	}
}

func TestDueInsideGrace(t *testing.T) {
	// 7:30 + 6h = 13:30 是边界，13:29 还算在窗内。
	now := d(2026, 11, 15, 13, 29)
	ok, why := Due(now, Config{Enabled: true, Hour: 7, Minute: 30}, "")
	if !ok {
		t.Fatalf("6 小时内该补跑（why=%q）", why)
	}
}

// 系统时钟被回拨，lastRun 落在未来。既不该补跑，也不该因为判断方式
// 变成「不等于今天」而立刻重跑。
func TestDueLastRunInFuture(t *testing.T) {
	now := d(2026, 11, 15, 9, 0)
	ok, _ := Due(now, Config{Enabled: true, Hour: 7, Minute: 30}, "2026-11-20")
	if ok {
		t.Fatal("上次记录在未来，说明时钟乱了，这时不该跑")
	}
}

func TestParseClock(t *testing.T) {
	cases := []struct {
		in    string
		h, m  int
		isErr bool
	}{
		{"07:30", 7, 30, false},
		{"7:05", 7, 5, false},
		{"", DefaultHour, DefaultMinute, false},
		{"24:00", 0, 0, true},
		{"07:60", 0, 0, true},
		{"abc", 0, 0, true},
	}
	for _, c := range cases {
		h, m, err := ParseClock(c.in)
		if c.isErr {
			if err == nil {
				t.Errorf("%q 应该报错", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q 不该报错: %v", c.in, err)
			continue
		}
		if h != c.h || m != c.m {
			t.Errorf("%q 解析成 %02d:%02d，期望 %02d:%02d", c.in, h, m, c.h, c.m)
		}
	}
}

func TestNormalize(t *testing.T) {
	c := Config{Hour: 99, Minute: -3}.Normalize()
	if c.Hour != DefaultHour || c.Minute != DefaultMinute {
		t.Fatalf("越界值没被掰回来: %+v", c)
	}
}

// 定时器最怕一天跑两次：那是实打实的配额浪费。
func TestTickRunsOncePerDay(t *testing.T) {
	now := d(2026, 11, 15, 7, 30)
	lastRun := ""
	runs := 0

	s := New(time.Minute)
	s.now = func() time.Time { return now }
	s.Load = func() (Config, string) { return Config{Enabled: true, Hour: 7, Minute: 30}, lastRun }
	s.Run = func(Config) (bool, error) {
		runs++
		lastRun = now.Format("2006-01-02") // 模拟落库
		return true, nil
	}

	s.Tick()
	s.Tick()
	s.Tick()

	if runs != 1 {
		t.Fatalf("一天只该跑一次，实际跑了 %d 次", runs)
	}
}

// 正忙被挡下时要退避，不能每分钟去抢一次锁。
func TestTickBacksOffWhenBusy(t *testing.T) {
	now := d(2026, 11, 15, 7, 30)
	attempts := 0

	s := New(time.Minute)
	s.now = func() time.Time { return now }
	s.Load = func() (Config, string) { return Config{Enabled: true, Hour: 7, Minute: 30}, "" }
	s.Run = func(Config) (bool, error) { attempts++; return false, nil }

	s.Tick()
	s.Tick()
	if attempts != 1 {
		t.Fatalf("忙的时候该退避，实际尝试了 %d 次", attempts)
	}

	now = now.Add(busyRetry)
	s.Tick()
	if attempts != 2 {
		t.Fatalf("退避结束后该重试，实际尝试了 %d 次", attempts)
	}
}

func TestNextRun(t *testing.T) {
	cfg := Config{Hour: 7, Minute: 30}
	// 还没到点：下次是今天。
	if got := NextRun(d(2026, 11, 15, 6, 0), cfg); got.Day() != 15 {
		t.Fatalf("6 点时下次应还是今天，得到 %v", got)
	}
	// 已过点：下次是明天。
	if got := NextRun(d(2026, 11, 15, 9, 0), cfg); got.Day() != 16 {
		t.Fatalf("9 点时下次应是明天，得到 %v", got)
	}
}
