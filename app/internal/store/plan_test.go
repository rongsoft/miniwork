package store

import (
	"testing"
	"time"

	"rongsoft/miniwork/app/internal/topic"
)

// seed 建一条指定状态的选题。走 SetStatus 是为了把事件也写进去——
// 卡住/流失的判定全靠这些事件，测试里也必须真实产生。
func seed(t *testing.T, st *Store, id string, to topic.Status) {
	t.Helper()
	if _, err := st.SaveTopic(Topic{
		ID: id, Title: "题 " + id, Platform: "douyin", Niche: "职场",
		Score: 80, ScoreReason: "测试", Source: "hotspot",
		Status: topic.Draft, CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("建选题失败: %v", err)
	}
	if to == topic.Draft {
		return
	}
	// 状态机不允许跨级（draft 不能直接到 scheduled），按顺序走。
	path := []topic.Status{}
	switch to {
	case topic.Picked:
		path = []topic.Status{topic.Picked}
	case topic.Writing:
		path = []topic.Status{topic.Picked, topic.Writing}
	case topic.Scheduled:
		path = []topic.Status{topic.Picked, topic.Scheduled}
	case topic.Published:
		path = []topic.Status{topic.Picked, topic.Writing, topic.Scheduled, topic.Published}
	case topic.Dropped:
		path = []topic.Status{topic.Dropped}
	}
	for _, s := range path {
		if err := st.SetStatus(id, s); err != nil {
			t.Fatalf("转到 %s 失败: %v", s, err)
		}
	}
}

func TestScheduleFromPicked(t *testing.T) {
	st := newTestStore(t)
	seed(t, st, "t1", topic.Picked)

	if err := st.Schedule("t1", "2026-11-20"); err != nil {
		t.Fatalf("排期失败: %v", err)
	}

	list, err := st.PlanOn("2026-11-20")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != "t1" {
		t.Fatalf("排期没落库: %+v", list)
	}
	if list[0].Status != topic.Scheduled {
		t.Fatalf("排期后状态应为 scheduled，得到 %s", list[0].Status)
	}
}

// draft 想直接排期要拦住：用户还没看过的选题不该被安排日子。
func TestScheduleRejectsDraft(t *testing.T) {
	st := newTestStore(t)
	seed(t, st, "t1", topic.Draft)

	err := st.Schedule("t1", "2026-11-20")
	if err == nil {
		t.Fatal("draft 直接排期应该被拒绝")
	}
}

func TestScheduleRejectsBadDate(t *testing.T) {
	st := newTestStore(t)
	seed(t, st, "t1", topic.Picked)

	if err := st.Schedule("t1", "2026/11/20"); err == nil {
		t.Fatal("非法日期应该被拒绝")
	}
}

// 已经排过、只改日子：状态没变，不能因为「状态未变化」报错。
func TestScheduleChangesDate(t *testing.T) {
	st := newTestStore(t)
	seed(t, st, "t1", topic.Scheduled)
	if err := st.Schedule("t1", "2026-11-20"); err != nil {
		t.Fatal(err)
	}
	if err := st.Schedule("t1", "2026-11-22"); err != nil {
		t.Fatalf("改排期失败: %v", err)
	}

	old, _ := st.PlanOn("2026-11-20")
	if len(old) != 0 {
		t.Fatalf("旧日期应清空，还有 %d 条", len(old))
	}
	now, _ := st.PlanOn("2026-11-22")
	if len(now) != 1 {
		t.Fatalf("新日期应有 1 条，得到 %d", len(now))
	}
}

// 取消排期退回「已收下」，不是退回待定：用户是「先不定哪天」，不是「不要了」。
func TestScheduleCancel(t *testing.T) {
	st := newTestStore(t)
	seed(t, st, "t1", topic.Scheduled)
	if err := st.Schedule("t1", "2026-11-20"); err != nil {
		t.Fatal(err)
	}
	if err := st.Schedule("t1", ""); err != nil {
		t.Fatalf("取消排期失败: %v", err)
	}

	list, _ := st.ListByStatus(topic.Picked, 10)
	if len(list) != 1 {
		t.Fatalf("取消后应回到 picked，得到 %d 条", len(list))
	}
	if list[0].PlanDate != "" {
		t.Fatalf("日期应被清空，得到 %q", list[0].PlanDate)
	}
}

func TestPlanBetween(t *testing.T) {
	st := newTestStore(t)
	seed(t, st, "a", topic.Scheduled)
	seed(t, st, "b", topic.Scheduled)
	seed(t, st, "c", topic.Picked) // 没排期，不该出现

	if err := st.Schedule("a", "2026-11-01"); err != nil {
		t.Fatal(err)
	}
	if err := st.Schedule("b", "2026-11-30"); err != nil {
		t.Fatal(err)
	}

	list, err := st.PlanBetween("2026-11-01", "2026-11-30")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("区间内应有 2 条，得到 %d", len(list))
	}
	// 按日期正序，日历渲染依赖这个顺序。
	if list[0].ID != "a" || list[1].ID != "b" {
		t.Fatalf("应按日期升序，得到 %s,%s", list[0].ID, list[1].ID)
	}
}

func TestOverdue(t *testing.T) {
	st := newTestStore(t)
	seed(t, st, "late", topic.Scheduled)
	seed(t, st, "today", topic.Scheduled)
	seed(t, st, "done", topic.Published)

	_ = st.Schedule("late", "2026-11-10")
	_ = st.Schedule("today", "2026-11-15")
	_ = st.Schedule("done", "2026-11-01")

	list, err := st.Overdue("2026-11-15")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != "late" {
		t.Fatalf("只有 late 算逾期（已发布的排除），得到 %+v", list)
	}
}

// DayGap 按自然日算，不是按 24 小时。晚上 23:50 到早上 00:10 是隔了一天。
func TestDayGap(t *testing.T) {
	a := time.Date(2026, 11, 14, 23, 50, 0, 0, time.Local)
	b := time.Date(2026, 11, 15, 0, 10, 0, 0, time.Local)
	if got := DayGap(a, b); got != 1 {
		t.Fatalf("跨零点应算 1 天，得到 %d", got)
	}
	if got := DayGap(time.Time{}, b); got != 0 {
		t.Fatalf("零值时间应返回 0，得到 %d", got)
	}
}

// StuckIn 靠 topic_event 算「停了多久」，这条链路错了待办和复盘都会失准。
// 第二个参数是「早于此刻进入当前状态的才算卡住」，所以阈值在过去=不捞，在未来=捞。
func TestStuckIn(t *testing.T) {
	st := newTestStore(t)
	seed(t, st, "w1", topic.Writing)

	// 阈值设在一分钟前：刚刚进入写作中的不小于它，不算卡住。
	if list, _ := st.StuckIn(topic.Writing, time.Now().Add(-time.Minute)); len(list) != 0 {
		t.Fatalf("刚进状态的不算卡住，得到 %d 条", len(list))
	}
	// 阈值设在明天：今天进入的小于它，能捞到，且带上进入时间。
	list, err := st.StuckIn(topic.Writing, time.Now().AddDate(0, 0, 1))
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("阈值在未来应能捞到，得到 %d 条", len(list))
	}
	if list[0].Since.IsZero() {
		t.Fatal("必须带回进入状态的时刻，否则算不出停了几天")
	}
}

// LastNiche 是记忆页与打分口径之间的桥。
// 它错了，跳过引导的用户会在记忆页看到赛道词被当成「他的偏好」。
func TestLastNiche(t *testing.T) {
	st := newTestStore(t)

	// 库里没东西时返回空，不能 panic 也不能返回假值。
	if got := st.LastNiche(); got != "" {
		t.Fatalf("空库应返回空串，得到 %q", got)
	}

	seed(t, st, "n1", topic.Draft)
	if got := st.LastNiche(); got != "职场" {
		t.Fatalf("应取到 seed 写入的赛道，得到 %q", got)
	}
}
