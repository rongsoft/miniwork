package store

import (
	"database/sql"
	"time"

	"rongsoft/miniwork/app/internal/topic"
)

// 本文件放「按时间维度问库」的查询。
//
// 它们全部基于两张表的两条时间线：
//   - topic.created_at：这条选题是什么时候冒出来的（供给侧）
//   - topic_event.at：人是什么时候做的决定（需求侧）
//
// 复盘和今日待办都从这两条线推出来，不需要另建统计表。
// 这是当初坚持「每次状态转移都留痕」的回报——现在一条历史数据都不缺。

// Event 是一次状态转移。
type Event struct {
	TopicID string
	From    string
	To      string
	At      time.Time
}

// EventsSince 取某时刻之后的所有状态转移，按时间正序。
func (s *Store) EventsSince(since time.Time) ([]Event, error) {
	rows, err := s.db.Query(`SELECT topic_id, from_status, to_status, at
		FROM topic_event WHERE at >= ? ORDER BY at`, since.Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Event
	for rows.Next() {
		var e Event
		var at string
		if err := rows.Scan(&e.TopicID, &e.From, &e.To, &at); err != nil {
			return nil, err
		}
		e.At, _ = time.Parse(time.RFC3339, at)
		out = append(out, e)
	}
	return out, rows.Err()
}

// CountCreatedSince 返回窗口内新产出的选题条数。
func (s *Store) CountCreatedSince(since time.Time) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM topic WHERE created_at >= ?`,
		since.Format(time.RFC3339)).Scan(&n)
	return n, err
}

// StatusCountsSince 返回窗口内产出的选题「现在」各状态分布。
//
// 注意这是「这批选题此刻在哪」，不是「这段时间发生了多少转移」。
// 漏斗要的是前者：上一周产的 20 条，现在有几条发出来了。
func (s *Store) StatusCountsSince(since time.Time) (map[string]int, error) {
	rows, err := s.db.Query(`SELECT status, COUNT(*) FROM topic
		WHERE created_at >= ? GROUP BY status`, since.Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]int{}
	for rows.Next() {
		var st string
		var n int
		if err := rows.Scan(&st, &n); err != nil {
			return nil, err
		}
		out[st] = n
	}
	return out, rows.Err()
}

// NewByDay 返回窗口内每天新增几条选题，键是 yyyy-mm-dd。
//
// created_at 存的是带时区的 RFC3339，前 10 个字符正好是写入当天的本地日期，
// 直接切串分组比在 SQL 里做日期函数更快也不会被时区绕住。
func (s *Store) NewByDay(since time.Time) (map[string]int, error) {
	rows, err := s.db.Query(`SELECT substr(created_at, 1, 10) AS d, COUNT(*)
		FROM topic WHERE created_at >= ? GROUP BY d`, since.Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]int{}
	for rows.Next() {
		var d string
		var n int
		if err := rows.Scan(&d, &n); err != nil {
			return nil, err
		}
		out[d] = n
	}
	return out, rows.Err()
}

// AIPair 是「模型给了多少分」与「人最后怎么处理」的配对。
// 复盘里判断模型准不准，靠的就是这两列的对照。
type AIPair struct {
	Fit    int
	Status string
}

// AIPairsSince 取走过模型的选题分数与状态。ai_score < 0 的不算——
// 那表示没经过模型，混进来会让校准结果失真。
func (s *Store) AIPairsSince(since time.Time) ([]AIPair, error) {
	rows, err := s.db.Query(`SELECT ai_score, status FROM topic
		WHERE ai_score >= 0 AND created_at >= ?`, since.Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AIPair
	for rows.Next() {
		var p AIPair
		if err := rows.Scan(&p.Fit, &p.Status); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// TopDropped 取窗口内被丢掉、但分数很高的选题。
//
// 这是复盘里最有用的一张表：它把「你当时的判断」摆在你面前。
// 一个月后回看，要么确认那会儿就该丢（判断没问题），
// 要么发现自己在成片回避某个方向（那才是真问题）。
func (s *Store) TopDropped(since time.Time, minScore, limit int) ([]Topic, error) {
	if limit <= 0 {
		limit = 5
	}
	return s.list(`WHERE status = ? AND score >= ? AND created_at >= ?
		ORDER BY score DESC LIMIT ?`,
		string(topic.Dropped), minScore, since.Format(time.RFC3339), limit)
}

// Aging 是一条「停着没动」的选题：它在当前状态待了多久。
type Aging struct {
	Topic
	Since time.Time // 进入当前状态的时刻
	Days  int       // 停了几天（按自然日算，不是 24h）
}

// StuckIn 取停在某状态、且已经停了超过 before 的选题。
//
// 「进入当前状态的时间」只能从 topic_event 取最近一条 to_status = 当前状态 的记录。
// topic 表里没有这个字段，也不该有——状态一多就要加一堆时间列。
func (s *Store) StuckIn(st topic.Status, before time.Time) ([]Aging, error) {
	rows, err := s.db.Query(`SELECT t.id, t.title, t.platform, t.niche, t.score,
			t.score_reason, t.ai_score, t.source, t.status, t.hotspot_id, t.heat,
			t.published_at, t.created_at, t.picked_at, t.plan_date, MAX(e.at) AS since
		FROM topic t
		JOIN topic_event e ON e.topic_id = t.id AND e.to_status = t.status
		WHERE t.status = ?
		GROUP BY t.id
		HAVING MAX(e.at) < ?
		ORDER BY MAX(e.at)`,
		string(st), before.Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	now := time.Now()
	var out []Aging
	for rows.Next() {
		var a Aging
		var status, created, published, since string
		var picked sql.NullString
		if err := rows.Scan(&a.ID, &a.Title, &a.Platform, &a.Niche, &a.Score,
			&a.ScoreReason, &a.AIScore, &a.Source, &status, &a.HotspotID, &a.Heat,
			&published, &created, &picked, &a.PlanDate, &since); err != nil {
			return nil, err
		}
		a.Status = topic.Status(status)
		a.PublishedAt, _ = time.Parse(time.RFC3339, published)
		a.CreatedAt, _ = time.Parse(time.RFC3339, created)
		a.Since, _ = time.Parse(time.RFC3339, since)
		if picked.Valid {
			if pt, perr := time.Parse(time.RFC3339, picked.String); perr == nil {
				a.PickedAt = &pt
			}
		}
		a.Days = DayGap(a.Since, now)
		out = append(out, a)
	}
	return out, rows.Err()
}

// DayGap 返回两个时刻之间跨了几个自然日。
//
// 不用 Sub().Hours()/24：晚上 23:50 到早上 00:10 只隔 20 分钟，但那是
// 「昨天的事拖到今天了」，对用户来说就是隔了一天。按日历日算才符合直觉。
func DayGap(from, to time.Time) int {
	if from.IsZero() {
		return 0
	}
	f := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, from.Location())
	t := time.Date(to.Year(), to.Month(), to.Day(), 0, 0, 0, 0, to.Location())
	d := int(t.Sub(f).Hours() / 24)
	if d < 0 {
		return 0
	}
	return d
}

// LastNiche 返回最近一次产出选题所用的赛道。
//
// 记忆页需要它。人设里记着用户选的赛道，但用户可能跳过引导没填——
// 这时人设是空的，而打分会用「界面上当前选的那个赛道」。
// 两处口径一旦不一致，记忆页就会把赛道词（汇报、领导……）当成学到的偏好列出来，
// 而实际打分里它们是明确排除的（赛道分已经算过）。
// 用户看到一份和实际生效不同的清单，比完全不显示更糟：他会按这份假账去删词。
func (s *Store) LastNiche() string {
	var n string
	_ = s.db.QueryRow(`SELECT niche FROM topic WHERE niche != ''
		ORDER BY created_at DESC LIMIT 1`).Scan(&n)
	return n
}
