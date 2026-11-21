package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Teardown 是一条存下来的拆解记录。
//
// Result 是整份 JSON（六行字段 + 可借鉴点），不拆列。理由写在 schema v10 的注释里：
// 字段会随提示词变，拆列的话每次改提示词都要迁移，历史数据还回填不了。
type Teardown struct {
	ID     string
	Title  string // 被拆的那条标题，原文
	Body   string // 用户粘进去的正文，留着「重新拆一次」
	Result string // JSON
	// Category 是 Result 里分类的冗余副本，只为列表能分组、能显示，不用解析 JSON。
	Category  string
	CreatedAt time.Time
}

// NewTeardownID 拆解记录没有天然唯一键（同一条爆款拆两次是两回事，
// 第二次可能换了提示词、换了模型），所以用随机 id。
func NewTeardownID() string { return newID("td-") }

// SaveTeardown 写入一条拆解记录。
func (s *Store) SaveTeardown(t Teardown) error {
	if strings.TrimSpace(t.ID) == "" {
		return fmt.Errorf("拆解记录缺 id")
	}
	_, err := s.db.Exec(`INSERT INTO teardown (id, title, body, result, category, created_at)
		VALUES (?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
			title    = excluded.title,
			body     = excluded.body,
			result   = excluded.result,
			category = excluded.category`,
		t.ID, t.Title, t.Body, t.Result, t.Category, t.CreatedAt.Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("写入拆解记录失败: %w", err)
	}
	return nil
}

// Teardowns 取最近拆过的记录。limit <= 0 时给 30 条。
//
// 不返回 body：列表只要标题和结果，把正文一起捞出来在几十条时就是几 MB 的浪费，
// 而正文只在「重新拆」时才需要，那一步单独按 id 取。
func (s *Store) Teardowns(limit int) ([]Teardown, error) {
	if limit <= 0 {
		limit = 30
	}
	rows, err := s.db.Query(`SELECT id, title, result, category, created_at
		FROM teardown ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("读取拆解记录失败: %w", err)
	}
	defer rows.Close()

	out := make([]Teardown, 0, limit)
	for rows.Next() {
		var t Teardown
		var at string
		if err := rows.Scan(&t.ID, &t.Title, &t.Result, &t.Category, &at); err != nil {
			return nil, err
		}
		t.CreatedAt, _ = time.Parse(time.RFC3339, at)
		out = append(out, t)
	}
	return out, rows.Err()
}

// TeardownByID 按 id 取一条，含正文。
func (s *Store) TeardownByID(id string) (Teardown, error) {
	var t Teardown
	var at string
	err := s.db.QueryRow(`SELECT id, title, body, result, category, created_at
		FROM teardown WHERE id = ?`, id).
		Scan(&t.ID, &t.Title, &t.Body, &t.Result, &t.Category, &at)
	if err == sql.ErrNoRows {
		return Teardown{}, fmt.Errorf("找不到这条拆解记录")
	}
	if err != nil {
		return Teardown{}, err
	}
	t.CreatedAt, _ = time.Parse(time.RFC3339, at)
	return t, nil
}

// DeleteTeardown 删除一条拆解记录。
func (s *Store) DeleteTeardown(id string) error {
	res, err := s.db.Exec(`DELETE FROM teardown WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("删除拆解记录失败: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("找不到这条拆解记录")
	}
	return nil
}
