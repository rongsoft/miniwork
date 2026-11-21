package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Profile 是「你是谁」——喂给模型的那几句话。
//
// 为什么是这几项而不是一堆标签：模型判断「这条适不适合你」真正需要的只有
// 四件事：你做哪行、你站在什么位置、给谁看、不能碰什么。字段再多了，
// 用户填不完，模型也读不过来。
type Profile struct {
	Niche       string // 赛道
	Positioning string // 一句话定位，例：二线城市的HR，专写职场潜规则
	Audience    string // 目标读者
	Tone        string // 语气风格
	Avoid       string // 禁忌/不想碰的话题
	UpdatedAt   time.Time
}

// Empty 判断用户是否还没填过。空画像时不能硬编一段假人设喂给模型，
// 那会让模型按「通用职场号」给分，反而比纯规则更差。
func (p Profile) Empty() bool {
	return p.Niche == "" && p.Positioning == "" && p.Audience == "" && p.Tone == "" && p.Avoid == ""
}

// Profile 读单行画像，没有记录时返回空值而不是错误。
func (s *Store) Profile() (Profile, error) {
	var p Profile
	var updated string
	err := s.db.QueryRow(`SELECT niche, positioning, audience, tone, avoid, updated_at
		FROM profile WHERE id = 1`).
		Scan(&p.Niche, &p.Positioning, &p.Audience, &p.Tone, &p.Avoid, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return Profile{}, nil
	}
	if err != nil {
		return Profile{}, fmt.Errorf("读取画像失败: %w", err)
	}
	p.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
	return p, nil
}

// SaveProfile 覆盖写入。人设只有一份，没有历史版本。
func (s *Store) SaveProfile(p Profile) error {
	p.UpdatedAt = time.Now()
	_, err := s.db.Exec(`INSERT INTO profile (id, niche, positioning, audience, tone, avoid, updated_at)
		VALUES (1,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
			niche = excluded.niche, positioning = excluded.positioning,
			audience = excluded.audience, tone = excluded.tone,
			avoid = excluded.avoid, updated_at = excluded.updated_at`,
		p.Niche, p.Positioning, p.Audience, p.Tone, p.Avoid, p.UpdatedAt.Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("保存画像失败: %w", err)
	}
	return nil
}

// Decisions 是「我做过什么决定」——记忆的第二层，也是唯一不需要用户手填的一层。
//
// 用户每收下一条、丢掉一条，模型下次就知道他偏什么。这是本地 Agent 相对
// 云端工具的真正优势：这些数据从来没离开过他的电脑，敢用。
type Decisions struct {
	Picked  []string
	Dropped []string
}

// RecentDecisions 取最近的决定，按时间倒序。
// 只取标题不取分数：模型要学的是「偏好」，不是「历史打分」。
func (s *Store) RecentDecisions(limit int) (Decisions, error) {
	if limit <= 0 {
		limit = 40
	}
	rows, err := s.db.Query(`SELECT title, status FROM topic
		WHERE status IN (?,?,?,?,?)
		ORDER BY COALESCE(picked_at, created_at) DESC LIMIT ?`,
		"picked", "writing", "scheduled", "published", "dropped", limit)
	if err != nil {
		return Decisions{}, err
	}
	defer rows.Close()

	var d Decisions
	seen := map[string]bool{}
	for rows.Next() {
		var title, status string
		if err := rows.Scan(&title, &status); err != nil {
			return Decisions{}, err
		}
		if seen[title] {
			continue
		}
		seen[title] = true
		if status == "dropped" {
			d.Dropped = append(d.Dropped, title)
		} else {
			d.Picked = append(d.Picked, title)
		}
	}
	return d, rows.Err()
}

// UpdateAIScore 写入模型分与最终分。
//
// 状态列一个都不碰：打分是机器的事，收下/丢掉是人的事，两者不能互相覆盖。
// reason 为空时保留原来的规则理由，避免模型返回半截 JSON 把说明清空。
// UpdateAIScore 写入模型分与最终分。
//
// factors 必须和 score 一起写：界面上四条维度条是从 factors 算的，
// 分数变了维度没变，用户会看到「78 分但匹配 95」这种自相矛盾的卡片。
// 传空串表示这次没有新的规则维度（只做了模型复核），保留库里原值。
func (s *Store) UpdateAIScore(id string, aiFit, final int, reason, factors string) error {
	var err error
	switch {
	case reason == "" && factors == "":
		_, err = s.db.Exec(`UPDATE topic SET ai_score = ?, score = ? WHERE id = ?`,
			aiFit, final, id)
	case factors == "":
		_, err = s.db.Exec(`UPDATE topic SET ai_score = ?, score = ?, score_reason = ? WHERE id = ?`,
			aiFit, final, reason, id)
	case reason == "":
		_, err = s.db.Exec(`UPDATE topic SET ai_score = ?, score = ?, factors = ? WHERE id = ?`,
			aiFit, final, factors, id)
	default:
		_, err = s.db.Exec(`UPDATE topic SET ai_score = ?, score = ?, score_reason = ?, factors = ? WHERE id = ?`,
			aiFit, final, reason, factors, id)
	}
	if err != nil {
		return fmt.Errorf("写入 AI 分失败: %w", err)
	}
	return nil
}

// UpdateScoreIfDraft 只更新「还没被用户处理过」的选题分数。
//
// 为什么需要它：用户改了人设（比如加了「不碰法律」），那条法律内容的分数
// 应该跟着掉下去。但它已经入库了，重跑取数时因为低于阈值会被跳过——
// 结果是旧的高分永远挂在列表上，用户会认为「我填的禁忌根本没生效」。
//
// 限定 draft 是安全边界：已经收下、正在写、已发布的选题，分数不该被人设变动改写，
// 那些记录承载的是用户做过的决定，不是待筛选的候选。
func (s *Store) UpdateScoreIfDraft(id string, score int, reason, factors string) error {
	q := `UPDATE topic SET score = ?`
	args := []any{score}
	if reason != "" {
		q += `, score_reason = ?`
		args = append(args, reason)
	}
	if factors != "" {
		q += `, factors = ?`
		args = append(args, factors)
	}
	q += ` WHERE id = ? AND status = 'draft'`
	args = append(args, id)
	if _, err := s.db.Exec(q, args...); err != nil {
		return fmt.Errorf("更新选题分数失败: %w", err)
	}
	return nil
}

// AICacheGet 取模型结果缓存。
//
// 用户点「重新生成」、崩溃重跑、调试期反复跑同一个赛道，都会重复花钱。
// 开发期这个命中率能到 50% 以上，等于额度翻倍。
func (s *Store) AICacheGet(key string) (string, bool, error) {
	var result string
	err := s.db.QueryRow(`SELECT result FROM ai_cache WHERE key = ?`, key).Scan(&result)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return result, true, nil
}

// AICachePut 写缓存。同 key 覆盖，保留最后一次结果。
func (s *Store) AICachePut(key, model, result string) error {
	_, err := s.db.Exec(`INSERT INTO ai_cache (key, model, result, created_at)
		VALUES (?,?,?,?)
		ON CONFLICT(key) DO UPDATE SET result = excluded.result, created_at = excluded.created_at`,
		key, model, result, time.Now().Format(time.RFC3339))
	return err
}

// Setting 的键名集中在这里，避免同一句话在两处拼写不一致。
const (
	SettingOnboarding = "onboarding_done" // "1" 表示引导走完了（含跳过）

	// 定时刷新。时间存 "07:30"，上次跑存 "2026-11-15"（日期，不是时刻）。
	// 存日期是因为要判「今天有没有过新数据」，而不是「上次是几点几分」。
	SettingScheduleEnabled = "schedule_enabled"
	SettingScheduleTime    = "schedule_time"
	SettingScheduleLastRun = "schedule_last_run"

	// 关窗后是否驻留在托盘。
	//
	// 存的是「已关闭」而不是「已开启」：缺省（键不存在）就当作开启，
	// 这样老库升上来不用迁移，用户装完就是「关窗不退出」——
	// 这正是我们要的默认行为。反过来存 tray_enabled 的话，
	// 缺省等于关闭，每个老用户升级后都得手动去点一下才生效。
	SettingTrayDisabled = "tray_disabled"

	// 用户手动屏蔽掉的偏好词，逗号分隔。
	//
	// 存的必要性：学出来的词是机器猜的，猜错时用户删掉它，
	// 下次学习必须别把同一个词又学回来——否则「删掉」这个动作毫无意义，
	// 用户会觉得这软件根本不听他的。
	SettingLearnedBlocked = "learned_blocked"
)

// BlockedWords 返回用户手动屏蔽的偏好词。
func (s *Store) BlockedWords() []string {
	raw := s.GetSetting(SettingLearnedBlocked)
	if raw == "" {
		return nil
	}
	var out []string
	for _, w := range strings.Split(raw, ",") {
		if w = strings.TrimSpace(w); w != "" {
			out = append(out, w)
		}
	}
	return out
}

// BlockLearntWord 屏蔽一个学到的词。
func (s *Store) BlockLearntWord(text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("要屏蔽的词不能为空")
	}
	list := s.BlockedWords()
	for _, w := range list {
		if w == text {
			return nil // 屏蔽两次是同一个意思，不用报错
		}
	}
	return s.SetSetting(SettingLearnedBlocked, strings.Join(append(list, text), ","))
}

// UnblockLearntWord 撤销屏蔽。
func (s *Store) UnblockLearntWord(text string) error {
	list := s.BlockedWords()
	kept := make([]string, 0, len(list))
	for _, w := range list {
		if w != text {
			kept = append(kept, w)
		}
	}
	return s.SetSetting(SettingLearnedBlocked, strings.Join(kept, ","))
}

// GetSetting 读一个界面状态。不存在时返回空串而不是错误：
// 「没设置过」和「读失败」对调用方的处理是一样的，没必要让调用方区分。
func (s *Store) GetSetting(key string) string {
	var v string
	if err := s.db.QueryRow(`SELECT value FROM app_setting WHERE key = ?`, key).Scan(&v); err != nil {
		return ""
	}
	return v
}

// SetSetting 写一个界面状态。
func (s *Store) SetSetting(key, value string) error {
	_, err := s.db.Exec(`INSERT INTO app_setting (key, value) VALUES (?,?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	if err != nil {
		return fmt.Errorf("写入设置失败: %w", err)
	}
	return nil
}
