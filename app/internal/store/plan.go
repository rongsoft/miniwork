package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"rongsoft/miniwork/app/internal/topic"
)

// DateLayout 是所有排期日期的唯一格式，别在别处再拼一遍。
const DateLayout = "2006-01-02"

// PlanDate 校验并规范化一个日期串。空串合法，表示「取消排期」。
func PlanDate(s string) (string, error) {
	if s == "" {
		return "", nil
	}
	t, err := time.Parse(DateLayout, s)
	if err != nil {
		return "", fmt.Errorf("日期格式应为 2026-11-15，收到 %q", s)
	}
	return t.Format(DateLayout), nil
}

// Schedule 把一条选题排到某天，空日期表示取消排期。
//
// 排期本身就是承诺：用户把它放到日历上，意思是「这天我要发它」。
// 所以 picked -> scheduled 是一步合法的转移，不必强迫他先点过「写作中」——
// MVP 里没有编辑器，写作中那个状态目前没有别的入口。
func (s *Store) Schedule(id, date string) error {
	date, err := PlanDate(date)
	if err != nil {
		return err
	}

	var cur string
	err = s.db.QueryRow(`SELECT status FROM topic WHERE id = ?`, id).Scan(&cur)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("选题不存在: %s", id)
	}
	if err != nil {
		return err
	}
	from := topic.Status(cur)

	// 取消排期：只对已排期的生效，退回「已收下」。
	// 不退回 draft：用户是「这条我要写但先不定哪天」，不是「不要了」。
	// 状态机为此专门放了 scheduled -> picked 这条边，含义就是「撤回排期」。
	// 日期必须一起清掉，否则会出现「已收下但日历上还占着一格」的鬼影。
	if date == "" && from == topic.Scheduled {
		return s.moveStatus(id, from, topic.Picked, "status = ?, plan_date = ''",
			string(topic.Picked))
	}
	if date == "" {
		// 其余状态本来就没有日期，清空是空操作，直接返回不报错。
		return nil
	}

	// 已经排过、只是改日子：状态没变，不走状态机（它会报「状态未变化」）。
	if from == topic.Scheduled {
		_, err := s.db.Exec(`UPDATE topic SET plan_date = ? WHERE id = ?`, date, id)
		if err != nil {
			return fmt.Errorf("改排期失败: %w", err)
		}
		return nil
	}

	// 其余情况必须先变成「已排期」，而状态机只允许从 picked / writing 过来。
	// draft 想直接排期，说明他还想再确认一眼，先让他收下。
	if !topic.Can(from, topic.Scheduled) {
		return fmt.Errorf("这条还在「%s」，先收下再排期", from.Label())
	}

	// 状态和日期一次写进去：分两步写，中间失败会留下「已排期但没日子」的行，
	// 那条选题会从日历和「今天要发」里同时消失。
	return s.moveStatus(id, from, topic.Scheduled, "status = ?, plan_date = ?",
		string(topic.Scheduled), date)
}

// PlanBetween 取某区间内排了期的选题，日历用。
//
// 不带状态过滤：已发布的也要显示，日历得是「我这段日子干了什么」，
// 而不是「我还没干的」。界面上按状态区分颜色即可。
func (s *Store) PlanBetween(from, to string) ([]Topic, error) {
	return s.list(`WHERE plan_date != '' AND plan_date BETWEEN ? AND ?
		ORDER BY plan_date, score DESC`, from, to)
}

// PlanOn 取某一天的排期，按状态排：没发的排前面。
func (s *Store) PlanOn(date string) ([]Topic, error) {
	return s.list(`WHERE plan_date = ? ORDER BY score DESC`, date)
}

// Overdue 取「排了今天之前、但还没发」的。这是待办里最该先看的一类：
// 已经承诺过日子的东西没交付，比还没决定的事严重。
func (s *Store) Overdue(before string) ([]Topic, error) {
	return s.list(`WHERE status = ? AND plan_date != '' AND plan_date < ?
		ORDER BY plan_date`, string(topic.Scheduled), before)
}
