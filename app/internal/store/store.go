// Package store 是本地唯一数据源（SQLite）。
//
// 约束：所有 SQL 走参数绑定；时间统一存 RFC3339 文本（字典序即时间序）。
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"rongsoft/miniwork/app/internal/source"
	"rongsoft/miniwork/app/internal/topic"
)

// Topic 是选题在库里的形态。
// heat 与 published_at 是内容快照：原始 hotspot 可能被清理，选题要能独立显示。
type Topic struct {
	ID          string
	Title       string
	Platform    string
	Niche       string
	Score       int
	ScoreReason string
	// AIScore 是模型给的分，-1 表示没走过模型。
	// 它和 Score 分开存，才能在复盘时看出「模型和规则谁更准」。
	AIScore     int
	Source      string
	Status      topic.Status
	HotspotID   string
	Heat        int64
	PublishedAt time.Time
	CreatedAt   time.Time
	PickedAt    *time.Time
	// PlanDate 是计划发布日 yyyy-mm-dd，空串表示没排期。
	// 用字符串而不是 time.Time：它是「哪一天」，没有时刻含义，
	// 转成 time.Time 只会引出「这个零点算哪个时区」的无谓问题。
	PlanDate string

	// Factors 是打分的四个维度，JSON 数组，形如
	// [{"name":"赛道相关","points":32,"max":40,"detail":"命中「裁员、仲裁」"}...]。
	// 空串表示这条是老数据或没走过规则打分——界面上就不显示维度条，
	// 而不是显示成四条 0。
	Factors string
}

// Store 包装 *sql.DB，只暴露业务需要的方法。
type Store struct {
	db *sql.DB
}

// Open 打开（或创建）本地库并跑迁移。
func Open(path string) (*Store, error) {
	dsn := "file:" + path +
		"?_pragma=journal_mode(WAL)" +
		"&_pragma=busy_timeout(5000)" +
		"&_pragma=foreign_keys(1)"

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}
	// SQLite 单写者模型，串行化最省心，也避免桌面端偶发 SQLITE_BUSY。
	db.SetMaxOpenConns(1)

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	var v int
	if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil {
		return fmt.Errorf("读取 schema 版本失败: %w", err)
	}
	for i := v; i < len(migrations); i++ {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		for _, stmt := range migrations[i] {
			if _, err := tx.Exec(stmt); err != nil {
				tx.Rollback()
				return fmt.Errorf("迁移到 v%d 失败: %w", i+1, err)
			}
		}
		if err := tx.Commit(); err != nil {
			tx.Rollback()
			return err
		}
		// 版本号来自代码常量，不涉及外部输入。
		if _, err := s.db.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, i+1)); err != nil {
			return fmt.Errorf("写入 schema 版本失败: %w", err)
		}
	}
	return nil
}

// SchemaVersion 返回当前库的版本，诊断包里要用。
func (s *Store) SchemaVersion() (int, error) {
	var v int
	err := s.db.QueryRow(`PRAGMA user_version`).Scan(&v)
	return v, err
}

// SaveHotspots 幂等写入。返回本次新增条数，重复抓取不会脏数据。
// niche 由调用方给定：它是「这次按什么赛道抓的」，不是内容自带字段。
func (s *Store) SaveHotspots(list []source.Hotspot, niche string) (int, error) {
	if len(list) == 0 {
		return 0, nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`INSERT OR IGNORE INTO hotspot
		(id, platform, title, author, url, heat, tags, published_at, fetched_at, niche)
		VALUES (?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	inserted := 0
	for _, h := range list {
		res, err := stmt.Exec(
			h.ID, h.Platform, h.Title, h.Author, h.URL, h.Heat,
			strings.Join(h.Tags, ","),
			h.PublishedAt.Format(time.RFC3339),
			h.FetchedAt.Format(time.RFC3339),
			niche,
		)
		if err != nil {
			return 0, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			inserted++
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return inserted, nil
}

// CachedHotspots 取本地缓存，用于数据源不可用时的降级。
//
// 只认 since 之后的抓取记录：三周前的选题不叫降级，叫过期。
// 顺带把空 niche 的老行排除掉，避免拿别的赛道内容冒充。
func (s *Store) CachedHotspots(niche string, since time.Time, limit int) ([]source.Hotspot, error) {
	if limit <= 0 {
		limit = 60
	}
	rows, err := s.db.Query(`SELECT id, platform, title, author, url, heat, tags,
		published_at, fetched_at FROM hotspot
		WHERE niche = ? AND fetched_at >= ?
		ORDER BY heat DESC LIMIT ?`,
		niche, since.Format(time.RFC3339), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []source.Hotspot
	for rows.Next() {
		var h source.Hotspot
		var tags, published, fetched string
		if err := rows.Scan(&h.ID, &h.Platform, &h.Title, &h.Author, &h.URL,
			&h.Heat, &tags, &published, &fetched); err != nil {
			return nil, err
		}
		if tags != "" {
			h.Tags = strings.Split(tags, ",")
		}
		h.PublishedAt, _ = time.Parse(time.RFC3339, published)
		h.FetchedAt, _ = time.Parse(time.RFC3339, fetched)
		out = append(out, h)
	}
	return out, rows.Err()
}

// SaveTopic 写入或更新一条选题。不覆盖已有状态（状态只能经 SetStatus 变更）。
//
// 返回值 created 表示「这条是本次新入池的」。调用方必须拿到它：
// 每轮都会把整批候选（含前几轮抓过的）重新打分，如果把「写入成功」当成
// 「新增」，界面会每天报「入选 20 条」，而用户看到的列表几乎没动——
// 这正是「刷新灵感没用」最伤人的地方：数字在骗人。
//
// 用 INSERT OR IGNORE 再补 UPDATE，而不是 ON CONFLICT DO UPDATE + RowsAffected：
// 后者对「插入了」和「更新了」都返回 1，分不出来。
func (s *Store) SaveTopic(t Topic) (created bool, err error) {
	res, err := s.db.Exec(`INSERT OR IGNORE INTO topic
		(id, title, platform, niche, score, score_reason, source, status,
		 hotspot_id, heat, published_at, created_at, factors)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		t.ID, t.Title, t.Platform, t.Niche, t.Score, t.ScoreReason,
		t.Source, string(t.Status), t.HotspotID, t.Heat,
		fmtTime(t.PublishedAt), t.CreatedAt.Format(time.RFC3339), t.Factors)
	if err != nil {
		return false, fmt.Errorf("写入选题失败: %w", err)
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return true, nil
	}

	// 已存在：只刷新分数与热度，不碰 status / created_at。
	// created_at 尤其不能动——它记的是「这条第一次进池的日期」，
	// 每次刷新把它改成 now，所有历史条目都会假装成今天的新东西。
	if _, err := s.db.Exec(`UPDATE topic SET
		score = ?, score_reason = ?, heat = ?, factors = ?
		WHERE id = ?`,
		t.Score, t.ScoreReason, t.Heat, t.Factors, t.ID); err != nil {
		return false, fmt.Errorf("刷新选题失败: %w", err)
	}
	return false, nil
}

// CountTitlesSince 统计某状态在 since 之后新建的「不同标题」条数。
//
// 数标题而不是数行：同一条内容两个平台各发一次是两行，界面上却只显示一张卡。
// 行数会比卡片多，首页那句「今天为你找到了 N 条」就会和列表数不出来——
// 这种对不上的小账最容易让人不信整页数据。
func (s *Store) CountTitlesSince(status topic.Status, since time.Time) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(DISTINCT title) FROM topic WHERE status = ? AND created_at >= ?`,
		string(status), since.Format(time.RFC3339)).Scan(&n)
	return n, err
}

// DraftSiblingIDs 取标题相同、状态为待定的其他选题 id。
//
// 同一条内容常被两个平台各发一次，派生出的选题 id 不同，列表里就成了两张卡。
// 用户在界面上只看到一张、只丢掉一张，另一张第二天又冒出来，
// 他会觉得「丢掉根本没生效」。所以收下/丢掉时要连同同标题的兄弟一起处理。
func (s *Store) DraftSiblingIDs(title, exceptID string) ([]string, error) {
	rows, err := s.db.Query(`SELECT id FROM topic
		WHERE status = ? AND title = ? AND id <> ?`,
		string(topic.Draft), strings.TrimSpace(title), exceptID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// TopDraft 取待定的前 N 条，按分数降序。
func (s *Store) TopDraft(limit int) ([]Topic, error) {
	return s.list(`WHERE status = ? ORDER BY score DESC LIMIT ?`, string(topic.Draft), limit)
}

// ListByStatus 按状态取列表，空字符串表示全部。
func (s *Store) ListByStatus(status topic.Status, limit int) ([]Topic, error) {
	if status == "" {
		return s.list(`ORDER BY created_at DESC LIMIT ?`, limit)
	}
	return s.list(`WHERE status = ? ORDER BY score DESC LIMIT ?`, string(status), limit)
}

// TopicByID 取一条选题。
//
// 生成稿件时标题必须从库里拿，不能靠界面传：界面上显示的标题可能是
// 几分钟前的列表快照，用户刚在别处改过，传过来的就是旧的。
func (s *Store) TopicByID(id string) (Topic, error) {
	list, err := s.list(`WHERE id = ?`, id)
	if err != nil {
		return Topic{}, err
	}
	if len(list) == 0 {
		return Topic{}, fmt.Errorf("选题不存在: %s", id)
	}
	return list[0], nil
}

func (s *Store) list(where string, args ...any) ([]Topic, error) {
	rows, err := s.db.Query(`SELECT id, title, platform, niche, score, score_reason,
		ai_score, source, status, hotspot_id, heat, published_at, created_at, picked_at, plan_date, factors
		FROM topic `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Topic
	for rows.Next() {
		var t Topic
		var status, created, published string
		var picked sql.NullString
		if err := rows.Scan(&t.ID, &t.Title, &t.Platform, &t.Niche, &t.Score,
			&t.ScoreReason, &t.AIScore, &t.Source, &status, &t.HotspotID, &t.Heat,
			&published, &created, &picked, &t.PlanDate, &t.Factors); err != nil {
			return nil, err
		}
		t.Status = topic.Status(status)
		t.PublishedAt, _ = time.Parse(time.RFC3339, published)
		t.CreatedAt, _ = time.Parse(time.RFC3339, created)
		if picked.Valid {
			if pt, err := time.Parse(time.RFC3339, picked.String); err == nil {
				t.PickedAt = &pt
			}
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// fmtTime 把零值时间写成空串，避免 0001-01-01 污染排序。
func fmtTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

// SetStatus 校验状态机后更新，并写一条 topic_event。两步在同一事务里。
func (s *Store) SetStatus(id string, to topic.Status) error {
	var cur string
	err := s.db.QueryRow(`SELECT status FROM topic WHERE id = ?`, id).Scan(&cur)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("选题不存在: %s", id)
	}
	if err != nil {
		return err
	}
	from := topic.Status(cur)
	if err := topic.Check(from, to); err != nil {
		return err
	}

	set := "status = ?"
	args := []any{string(to)}
	switch to {
	case topic.Picked:
		set += ", picked_at = ?"
		args = append(args, time.Now().Format(time.RFC3339))
	case topic.Draft:
		// 退回待定等于撤销「收下」这个决定，收下时间也要一起清掉，
		// 否则「我决定过的」这条记录会留在库里，和状态自相矛盾。
		set += ", picked_at = NULL"
	}
	return s.moveStatus(id, from, to, set, args...)
}

// moveStatus 是「改状态 + 留痕」的唯一实现。
//
// 单独抽出来是因为还有别的动作会改状态（比如排期顺带写日期、撤回排期顺带清日期）。
// 让每个动作各写一遍 INSERT INTO topic_event，迟早有一个分支漏掉——而事件表
// 一旦缺行，复盘和待办就会静默算错，那种错是不会报错的。
//
// set 是 UPDATE 里 "status = ?" 之后的部分，args 应对应它的占位符；
// 调用方负责保证 set 里已经包含 status 列的赋值。
func (s *Store) moveStatus(id string, from, to topic.Status, set string, args ...any) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	q := append(append([]any{}, args...), id)
	if _, err := tx.Exec(`UPDATE topic SET `+set+` WHERE id = ?`, q...); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO topic_event (topic_id, from_status, to_status, at)
		VALUES (?,?,?,?)`, id, string(from), string(to),
		time.Now().Format(time.RFC3339)); err != nil {
		return err
	}
	return tx.Commit()
}

// Stats 返回各状态的数量，用于控制台和 UI 顶部。
func (s *Store) Stats() (map[string]int, error) {
	rows, err := s.db.Query(`SELECT status, COUNT(*) FROM topic GROUP BY status`)
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

// CountHotspots 返回库里的内容条数。
func (s *Store) CountHotspots() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM hotspot`).Scan(&n)
	return n, err
}

// NewTopicID 用内容 id 派生选题 id，保证同一条内容不会重复生成选题。
func NewTopicID(hotspotID string) string { return "t-" + hotspotID }

// FillFactors 只写打分维度，不碰分数。
//
// 为什么单独开一个方法而不是复用 UpdateScoreIfDraft：那两个方法一次会写
// score / score_reason，用在「补维度」上会把模型加权后的分数降级成规则分。
// 数据迁移只该补缺的列，不该顺带重算别的列——那是一次静默的数据破坏。
func (s *Store) FillFactors(id, factors string) error {
	if factors == "" {
		return nil
	}
	_, err := s.db.Exec(`UPDATE topic SET factors = ? WHERE id = ?`, factors, id)
	return err
}
