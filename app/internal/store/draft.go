package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Draft 是一篇稿子。
//
// 与 Topic 的区别要守住：Topic 是「我打算写什么」，Draft 是「我写出来的东西」。
// 一篇稿子里出现的字，都能被复制粘贴到平台上发出去；topic 里不行。
//
// Format 只有两个取值：long（图文长文）与 short（短视频口播）。
// 不用「平台」直接决定形态——公众号也能发短的，小红书也能发长图文，
// 平台给默认值，改不改由用户决定。
type Draft struct {
	ID       string
	TopicID  string
	Platform string
	Format   string
	Title    string
	Body     string
	Tags     []string
	// Segments 是口播稿的分镜，图文长文为空。
	// 存 JSON 文本而不是嵌套表：它永远整段读写，没有单查的需求。
	Segments  string
	Note      string // 生成时的一句话说明：这一稿的角度是什么
	Edited    bool   // 人改过没有
	CreatedAt time.Time
	UpdatedAt time.Time
}

// PublishRecord 是一条发布记录。
//
// URL 允许为空：小红书很多人不填链接，只在自己主页看数据。
// 指标用 -1 表示未回填，「没回填」和「数据是 0」必须能分开，
// 否则算平均播放时会被一堆假零拉垮。
type PublishRecord struct {
	ID          string
	TopicID     string
	DraftID     string
	Platform    string
	Account     string
	URL         string
	PublishedAt string // yyyy-mm-dd
	Note        string
	Views       int
	Likes       int
	Collects    int
	Comments    int
	// Rating 是复盘时自己打的星（1–5），0 表示还没复盘。
	// 与上面四个指标刻意放在同一行：主观判断和客观结果必须能对齐着看。
	Rating    int
	Analysis  string // 这一次的复盘：为什么好 / 为什么差
	Lesson    string // 抽出来的一句可复用经验
	// MetricsSrc 是这四个数的来源：auto（平台自动抓）/ manual（自己填）/ 空（还没数）。
	// 手抄会抄错、会抄成前一天的数，界面必须能区分——不然一列数字看着一样，
	// 可信度差得远。
	MetricsSrc string
	MetricsAt  string // 最近一次自动取数的时间
	CreatedAt  time.Time
}

// newID 生成带前缀的随机 id。
//
// 不用时间戳：同一秒内连点两次「生成」，时间戳方案会撞 id，
// 后一稿会静默覆盖前一稿——而用户以为自己在对比两个角度。
func newID(prefix string) string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand 在桌面端基本不会失败；真失败也必须有 id，
		// 用纳秒兜底，碰撞概率远低于「直接报错让用户无法继续」的代价。
		return fmt.Sprintf("%s%d", prefix, time.Now().UnixNano())
	}
	return prefix + hex.EncodeToString(b[:])
}

// stamp 是稿件时间戳的唯一格式，带固定 3 位毫秒。
//
// 为什么不用库其他地方那套 RFC3339（秒精度）：用户点「生成」之后
// 再点一次「换个角度」，两稿会落在同一秒里，created_at 完全一样，
// 列表顺序就变成随机的——他会看到「刚生成的那篇跑到下面去了」。
// 固定 3 位小数还有个好处：字典序仍然等于时间序，SQL 排序不用改。
func stamp() string { return time.Now().Format("2006-01-02T15:04:05.000Z07:00") }

// SaveDraft 写入或更新一篇稿子。id 为空时新建。
func (s *Store) SaveDraft(d Draft) (string, error) {
	now := stamp()
	tags := strings.Join(d.Tags, ",")

	if strings.TrimSpace(d.ID) == "" {
		d.ID = newID("d-")
		_, err := s.db.Exec(`INSERT INTO draft
			(id, topic_id, platform, format, title, body, tags, segments, note, edited, created_at, updated_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
			d.ID, d.TopicID, d.Platform, d.Format, d.Title, d.Body, tags,
			d.Segments, d.Note, boolInt(d.Edited), now, now)
		if err != nil {
			return "", fmt.Errorf("保存稿件失败: %w", err)
		}
		return d.ID, nil
	}

	// 更新时不动 created_at：它是「这一稿什么时候有的」，
	// 每次编辑都刷新的话，「最新一稿」的判断会变成「最后被打开过的一稿」。
	res, err := s.db.Exec(`UPDATE draft
		SET platform = ?, format = ?, title = ?, body = ?, tags = ?, segments = ?,
		    note = ?, edited = ?, updated_at = ?
		WHERE id = ?`,
		d.Platform, d.Format, d.Title, d.Body, tags, d.Segments,
		d.Note, boolInt(d.Edited), now, d.ID)
	if err != nil {
		return "", fmt.Errorf("更新稿件失败: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return "", fmt.Errorf("稿件不存在: %s", d.ID)
	}
	return d.ID, nil
}

// DraftByID 取一篇稿子。不存在时返回 sql.ErrNoRows 的包装，调用方按「没有」处理。
func (s *Store) DraftByID(id string) (Draft, error) {
	row := s.db.QueryRow(`SELECT `+draftCols+` FROM draft WHERE id = ?`, id)
	return scanDraft(row)
}

// DraftsByTopic 取某条选题的所有稿子，新的在前。
func (s *Store) DraftsByTopic(topicID string) ([]Draft, error) {
	// 次级键用 rowid：同一毫秒内连写两稿时，时间戳一样，
	// 只有自增的 rowid 知道谁后写。
	rows, err := s.db.Query(`SELECT `+draftCols+` FROM draft
		WHERE topic_id = ? ORDER BY created_at DESC, rowid DESC`, topicID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDrafts(rows)
}

// RecentDrafts 取最近写过的稿子，用于「最近成稿」列表。
func (s *Store) RecentDrafts(limit int) ([]Draft, error) {
	if limit <= 0 {
		limit = 30
	}
	rows, err := s.db.Query(`SELECT `+draftCols+` FROM draft
		ORDER BY created_at DESC, rowid DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDrafts(rows)
}

// DraftedTopicIDs 返回已经有稿的选题 id，用来在待发队列里标「已备稿」。
//
// 一次查全量而不是逐个查：队列最多几十条，逐条 round-trip 是几十次
// 数据库往返，用户在切页时能明显感到卡一下。
func (s *Store) DraftedTopicIDs() (map[string]bool, error) {
	rows, err := s.db.Query(`SELECT DISTINCT topic_id FROM draft`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// DeleteDraft 删掉一稿。已发布过的稿子连着台账，但台账里有 draft_id，
// 删稿不会连带删台账——记录「我发过什么」比「稿子还在不在」重要。
func (s *Store) DeleteDraft(id string) error {
	res, err := s.db.Exec(`DELETE FROM draft WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("稿件不存在: %s", id)
	}
	return nil
}

// MarkDraftEdited 只改 edited 标记。
// 单独一个方法是因为它由「用户保存」触发，而保存时前端并不知道
// 自己是改了一个字还是重写了整段——都没关系，改过就是改过。
func (s *Store) MarkDraftEdited(id string) error {
	_, err := s.db.Exec(`UPDATE draft SET edited = 1, updated_at = ? WHERE id = ?`,
		stamp(), id)
	return err
}

const draftCols = `id, topic_id, platform, format, title, body, tags, segments, note, edited, created_at, updated_at`

func scanDraft(row *sql.Row) (Draft, error) {
	var d Draft
	var tags, created, updated string
	var edited int
	err := row.Scan(&d.ID, &d.TopicID, &d.Platform, &d.Format, &d.Title, &d.Body,
		&tags, &d.Segments, &d.Note, &edited, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return Draft{}, fmt.Errorf("稿件不存在")
	}
	if err != nil {
		return Draft{}, err
	}
	d.Tags = splitTags(tags)
	d.Edited = edited != 0
	d.CreatedAt, _ = time.Parse(time.RFC3339, created)
	d.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
	return d, nil
}

func scanDrafts(rows *sql.Rows) ([]Draft, error) {
	var out []Draft
	for rows.Next() {
		var d Draft
		var tags, created, updated string
		var edited int
		if err := rows.Scan(&d.ID, &d.TopicID, &d.Platform, &d.Format, &d.Title, &d.Body,
			&tags, &d.Segments, &d.Note, &edited, &created, &updated); err != nil {
			return nil, err
		}
		d.Tags = splitTags(tags)
		d.Edited = edited != 0
		d.CreatedAt, _ = time.Parse(time.RFC3339, created)
		d.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
		out = append(out, d)
	}
	return out, rows.Err()
}

// splitTags 清掉空串。标签里夹一个空元素会在界面上变成一个空白 chip，
// 复制出去就是「##」，用户还得手工删。
func splitTags(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var out []string
	for _, t := range strings.Split(s, ",") {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ---------- 发布台账 ----------

// AddPublish 记一条发布。同一个选题发多个平台就是多条记录。
//
// 四个指标在这里被强制写成 -1，不看调用方传了什么：刚记下的发布
// 不可能已经有数据。把「记得传 -1」这件事交给调用方，早晚有一处漏传，
// 而漏传的后果是一条「播放 0」混进复盘均值里，看不出来但一直拉低结论。
func (s *Store) AddPublish(p PublishRecord) (string, error) {
	if strings.TrimSpace(p.ID) == "" {
		p.ID = newID("p-")
	}
	if strings.TrimSpace(p.PublishedAt) == "" {
		p.PublishedAt = time.Now().Format(DateLayout)
	}
	_, err := s.db.Exec(`INSERT INTO publish_record
		(id, topic_id, draft_id, platform, account, url, published_at, note,
		 views, likes, collects, comments, created_at)
		VALUES (?,?,?,?,?,?,?,?,-1,-1,-1,-1,?)`,
		p.ID, p.TopicID, p.DraftID, p.Platform, p.Account, p.URL,
		p.PublishedAt, p.Note, stamp())
	if err != nil {
		return "", fmt.Errorf("记录发布失败: %w", err)
	}
	return p.ID, nil
}

// ListPublish 取台账，新的在前。
func (s *Store) ListPublish(limit int) ([]PublishRecord, error) {
	if limit <= 0 {
		limit = 60
	}
	rows, err := s.db.Query(`SELECT id, topic_id, draft_id, platform, account, url,
		published_at, note, views, likes, collects, comments, rating, analysis, lesson,
		metrics_src, metrics_at, created_at
		FROM publish_record ORDER BY published_at DESC, created_at DESC, rowid DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []PublishRecord
	for rows.Next() {
		var p PublishRecord
		var created string
		if err := rows.Scan(&p.ID, &p.TopicID, &p.DraftID, &p.Platform, &p.Account,
			&p.URL, &p.PublishedAt, &p.Note, &p.Views, &p.Likes, &p.Collects,
			&p.Comments, &p.Rating, &p.Analysis, &p.Lesson,
			&p.MetricsSrc, &p.MetricsAt, &created); err != nil {
			return nil, err
		}
		p.CreatedAt, _ = time.Parse(time.RFC3339, created)
		out = append(out, p)
	}
	return out, rows.Err()
}

// UpdateMetrics 回填一条记录的四个数。传 -1 表示这一项还没数，保持原样。
//
// 用 -1 而不是 0 当「不改」：0 是合法数据（真没人看），
// 若用 0 当哨兵，用户永远没法把一个数改回 0。
func (s *Store) UpdateMetrics(id string, views, likes, collects, comments int) error {
	sets := []string{}
	args := []any{}
	for _, f := range []struct {
		col string
		val int
	}{
		{"views", views}, {"likes", likes},
		{"collects", collects}, {"comments", comments},
	} {
		if f.val >= 0 {
			sets = append(sets, f.col+" = ?")
			args = append(args, f.val)
		}
	}
	if len(sets) == 0 {
		return nil
	}
	// 手填也要落来源：一行里只要有一项是人改的，这一列的可信度就归人工，
	// 界面据此显示「手动」，而不是把它和平台数据混成一样的样子。
	sets = append(sets, `metrics_src = 'manual'`)
	args = append(args, id)
	res, err := s.db.Exec(`UPDATE publish_record SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("发布记录不存在: %s", id)
	}
	return nil
}

// MetricPatch 是一条自动取到的数据。
type MetricPatch struct {
	ID       string
	Views    int
	Likes    int
	Collects int
	Comments int
	URL      string // 本地没记链接、平台给了，就补上
}

// ApplyMetrics 把自动取到的数写回记录，并标记来源。
//
// 批量写在一个事务里：一次同步可能改几十行，中途失败留下「一半是新的、
// 一半是旧的」比整批不写更难解释——用户会看到同一批笔记里有的数是今天的、
// 有的是上周的。
//
// 负数项跳过：平台没返回这一项，不等于这一项是 0。
func (s *Store) ApplyMetrics(ps []MetricPatch) (int, error) {
	if len(ps) == 0 {
		return 0, nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	now := stamp()
	n := 0
	for _, p := range ps {
		sets := []string{`metrics_src = 'auto'`, `metrics_at = ?`}
		args := []any{now}
		for _, f := range []struct {
			col string
			val int
		}{
			{"views", p.Views}, {"likes", p.Likes},
			{"collects", p.Collects}, {"comments", p.Comments},
		} {
			if f.val >= 0 {
				sets = append(sets, f.col+" = ?")
				args = append(args, f.val)
			}
		}
		if strings.TrimSpace(p.URL) != "" {
			// 只补空，不覆盖：用户自己填过的链接可能是短链或带了参数，
			// 平台给的那条反而不好分享。
			sets = append(sets, `url = CASE WHEN url = '' THEN ? ELSE url END`)
			args = append(args, p.URL)
		}
		args = append(args, p.ID)
		res, err := tx.Exec(`UPDATE publish_record SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...)
		if err != nil {
			return 0, err
		}
		if c, _ := res.RowsAffected(); c > 0 {
			n++
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return n, nil
}

// PublishStats 是台账顶部那几个数。只统计已回填的，
// 用 SUM + CASE 而不是把行捞回内存再算：台账会越积越多。
// PublishTotals 是台账的合计。
//
// Filled 在库里数，不是「界面上取出来的那几十条里有几条填了」：
// 台账只显示最近 60 条，拿可见条数当分母会算出「55/120」这种自相矛盾的比例，
// 用户会以为有一半数据丢了。
type PublishTotals struct {
	Total    int // 一共记过几条发布
	Views    int // 已回填的播放之和（未回填按 0，不参与、也不报错）
	Likes    int
	Collects int
	Filled   int // 至少填了一项的有几条
	Rated    int // 打过高星的有几条（rating > 0）
	StarSum  int // 星级总和，算平均分用
	Lessons  int // 沉淀了可复用经验的有几条
	// Pending 是「有数据但还没复盘」的条数。在库里数而不是在界面列表里数：
	// 列表只取最近 60 条，拿可见条数当分母会算出「8/120」这种自相矛盾的数。
	Pending int
}

func (s *Store) PublishStats() (PublishTotals, error) {
	var t PublishTotals
	// filled 这段条件出现两次（Filled 与 Pending），抽成常量免得改一处漏一处。
	const filled = `views >= 0 OR likes >= 0 OR collects >= 0 OR comments >= 0`
	err := s.db.QueryRow(`SELECT COUNT(*),
		COALESCE(SUM(CASE WHEN views    >= 0 THEN views    ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN likes    >= 0 THEN likes    ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN collects >= 0 THEN collects ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN `+filled+` THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN rating > 0 THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN rating > 0 THEN rating ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN lesson <> '' THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN (`+filled+`) AND rating = 0 THEN 1 ELSE 0 END), 0)
		FROM publish_record`).
		Scan(&t.Total, &t.Views, &t.Likes, &t.Collects, &t.Filled,
			&t.Rated, &t.StarSum, &t.Lessons, &t.Pending)
	return t, err
}

// SaveNoteReview 存一次复盘：星级、分析、可复用经验。
//
// 三样一次写完，不拆成三个方法：它们在界面上是同一张表单的一次提交，
// 分三次调用必然出现「星打了、分析没存进去」的半截状态，
// 而用户看到的是「我明明写了」。
//
// rating 传 0 表示撤掉评分（重新回到待复盘），不是「打 0 星」——星级从 1 起。
func (s *Store) SaveNoteReview(id string, rating int, analysis, lesson string) error {
	if rating < 0 || rating > 5 {
		return fmt.Errorf("星级只能是 1–5，或者 0 表示撤销评分")
	}
	res, err := s.db.Exec(`UPDATE publish_record
		SET rating = ?, analysis = ?, lesson = ?
		WHERE id = ?`,
		rating, strings.TrimSpace(analysis), strings.TrimSpace(lesson), id)
	if err != nil {
		return fmt.Errorf("存复盘失败: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("发布记录不存在: %s", id)
	}
	return nil
}
