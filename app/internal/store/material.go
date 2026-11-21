package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Material 是素材库里的一条。
//
// 它和 Topic 的区别是「方向相反」：选题是「我要写什么」，素材是
// 「别人怎么写的、我存下来学」。所以素材有 URL 和作者，选题没有；
// 选题有分数和状态机，素材没有。
type Material struct {
	ID       string
	URL      string
	Platform string
	Author   string
	Title    string
	// Body 是用户粘进来的正文。空表示只存了链接。
	Body string
	// Summary 是 AI 摘要，空表示还没提炼。
	Summary string
	// Points 是「值得学习的点」，JSON 数组。
	Points    string
	Likes     int
	Collects  int
	Status    string // raw 待提炼 | torn 已提炼
	TopicID   string // 关联的选题，空表示还没关联
	CreatedAt time.Time
}

// 素材状态。只有两态：提炼过没有。
//
// 不做「已归档」这类状态：素材的用途是「写的时候翻一下」，
// 用得少了自然沉底，不需要用户手动归档来维持整洁。
const (
	MaterialRaw  = "raw"
	MaterialTorn = "torn"
)

// orRaw 把空状态兜底成待提炼。
//
// 调用方给空串是常态（新建时没人愿意写 status），让它落成空串的话，
// 统计查询里 status != 'torn' 的分支会把这条算成待提炼——看着对，
// 但库里的值是个既不是 raw 也不是 torn 的第三种东西，以后加状态就会出错。
func orRaw(s string) string {
	if strings.TrimSpace(s) == "" {
		return MaterialRaw
	}
	return s
}

// NewMaterialID 由 URL 派生：同一条链接存两次得到同一个 id，
// 靠主键冲突去重，不用先查一次。
//
// URL 为空时退回「平台+标题+时间戳」：那种情况是手工录入，
// 没有天然唯一键，用时间戳保证能存进去，重复由用户自己看着办。
func NewMaterialID(url, platform, title string, now time.Time) string {
	if u := strings.TrimSpace(url); u != "" {
		sum := sha256.Sum256([]byte(u))
		return "m-" + hex.EncodeToString(sum[:8])
	}
	sum := sha256.Sum256([]byte(platform + "|" + title + "|" + now.Format(time.RFC3339Nano)))
	return "m-" + hex.EncodeToString(sum[:8])
}

// SaveMaterial 写入或更新一条素材。
//
// 更新时只覆盖用户能改的字段：summary 和 points 由 AI 流程单独写，
// 这里带进来会变成「用户改个标题就把摘要清空了」。
func (s *Store) SaveMaterial(m Material) error {
	_, err := s.db.Exec(`INSERT INTO material
		(id, url, platform, author, title, body, summary, points,
		 likes, collects, status, topic_id, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
			url      = excluded.url,
			platform = excluded.platform,
			author   = excluded.author,
			title    = excluded.title,
			body     = excluded.body,
			likes    = excluded.likes,
			collects = excluded.collects`,
		m.ID, m.URL, m.Platform, m.Author, m.Title, m.Body,
		m.Summary, m.Points, m.Likes, m.Collects,
		orRaw(m.Status), m.TopicID,
		m.CreatedAt.Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("写入素材失败: %w", err)
	}
	return nil
}

// Materials 取素材列表。platform 为空表示全部。
func (s *Store) Materials(platform string, limit int) ([]Material, error) {
	if limit <= 0 {
		limit = 100
	}
	q := `SELECT id, url, platform, author, title, body, summary, points,
			likes, collects, status, topic_id, created_at FROM material`
	args := []any{}
	if p := strings.TrimSpace(platform); p != "" {
		q += ` WHERE platform = ?`
		args = append(args, p)
	}
	q += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)
	return s.scanMaterials(q, args...)
}

// MaterialByID 取一条。
func (s *Store) MaterialByID(id string) (Material, error) {
	list, err := s.scanMaterials(`SELECT id, url, platform, author, title, body, summary, points,
		likes, collects, status, topic_id, created_at FROM material WHERE id = ?`, id)
	if err != nil {
		return Material{}, err
	}
	if len(list) == 0 {
		return Material{}, fmt.Errorf("素材不存在: %s", id)
	}
	return list[0], nil
}

func (s *Store) scanMaterials(q string, args ...any) ([]Material, error) {
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Material
	for rows.Next() {
		var m Material
		var created string
		if err := rows.Scan(&m.ID, &m.URL, &m.Platform, &m.Author, &m.Title,
			&m.Body, &m.Summary, &m.Points, &m.Likes, &m.Collects,
			&m.Status, &m.TopicID, &created); err != nil {
			return nil, err
		}
		m.CreatedAt, _ = time.Parse(time.RFC3339, created)
		out = append(out, m)
	}
	return out, rows.Err()
}

// SetMaterialDigest 写入 AI 提炼结果，状态一起改成已提炼。
//
// 摘要和要点必须同一个事务里写：只写摘要不写要点，界面上就会出现
// 「有摘要但没有学习点」的半成品，用户会以为是提炼失败。
func (s *Store) SetMaterialDigest(id, summary string, points []string) error {
	raw, err := json.Marshal(points)
	if err != nil {
		return fmt.Errorf("编码学习点失败: %w", err)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE material SET summary = ?, points = ?, status = ? WHERE id = ?`,
		summary, string(raw), MaterialTorn, id); err != nil {
		return err
	}
	return tx.Commit()
}

// SetMaterialTopic 关联选题。传空串表示取消关联。
func (s *Store) SetMaterialTopic(id, topicID string) error {
	_, err := s.db.Exec(`UPDATE material SET topic_id = ? WHERE id = ?`, topicID, id)
	if err != nil {
		return fmt.Errorf("关联选题失败: %w", err)
	}
	return nil
}

// DeleteMaterial 删一条素材。
func (s *Store) DeleteMaterial(id string) error {
	_, err := s.db.Exec(`DELETE FROM material WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("删除素材失败: %w", err)
	}
	return nil
}

// MaterialStat 是素材页顶部那四个数。
type MaterialStat struct {
	Total    int
	Platform map[string]int // 各平台条数，筛选按钮上显示
	Torn     int            // 已提炼
	Raw      int            // 待提炼
	Linked   int            // 已关联选题
}

// MaterialStats 一次查出四个统计。用一条 SQL 而不是四次：
// 这张表不大，但四次往返换来的是四个可能不一致的快照。
func (s *Store) MaterialStats() (MaterialStat, error) {
	st := MaterialStat{Platform: map[string]int{}}
	rows, err := s.db.Query(`SELECT platform, status, topic_id <> '' FROM material`)
	if err != nil {
		return st, err
	}
	defer rows.Close()
	for rows.Next() {
		var plat, status string
		var linked bool
		if err := rows.Scan(&plat, &status, &linked); err != nil {
			return st, err
		}
		st.Total++
		st.Platform[plat]++
		if status == MaterialTorn {
			st.Torn++
		} else {
			st.Raw++
		}
		if linked {
			st.Linked++
		}
	}
	return st, rows.Err()
}
