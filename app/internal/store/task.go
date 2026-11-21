package store

import (
	"time"
)

// Task 是一条排好了、还没发的发布任务。
//
// 字段取舍见 schema.go 里 v13 那段：title/platform/format 是快照，
// 草稿后来被改、被删都不影响这条任务当时的决定。
type Task struct {
	ID        string
	DraftID   string
	TopicID   string
	Title     string
	Platform  string
	Format    string
	Account   string
	Status    string // todo / done
	Seq       int
	CreatedAt time.Time
}

// TaskID 是给 service 层用的 id 生成器：newID 在包内不导出。
func TaskID() string { return newID("t_") }

// AddTasks 整批写入。
//
// 用事务而不是逐条 Exec：一次生成十来个任务，中途失败留下半批会让用户
// 看到「生成了 7 条」而不知道少的 3 条去哪了；全成或全不成，才好解释。
func (s *Store) AddTasks(list []Task) error {
	if len(list) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO pub_task (id, draft_id, topic_id, title, platform, format, account, status, seq, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, t := range list {
		status := t.Status
		if status == "" {
			status = "todo"
		}
		if _, err := stmt.Exec(t.ID, t.DraftID, t.TopicID, t.Title, t.Platform, t.Format,
			t.Account, status, t.Seq, t.CreatedAt.Format(time.RFC3339)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Tasks 按 seq 返回某状态的任务。seq 是生成时的顺序，人排的时候就按这个顺序发。
func (s *Store) Tasks(status string) ([]Task, error) {
	q := `SELECT id, draft_id, topic_id, title, platform, format, account, status, seq, created_at FROM pub_task`
	var args []any
	if status != "" {
		q += ` WHERE status = ?`
		args = append(args, status)
	}
	q += ` ORDER BY seq ASC, created_at ASC`

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Task
	for rows.Next() {
		var t Task
		var created string
		if err := rows.Scan(&t.ID, &t.DraftID, &t.TopicID, &t.Title, &t.Platform, &t.Format,
			&t.Account, &t.Status, &t.Seq, &created); err != nil {
			return nil, err
		}
		t.CreatedAt, _ = time.Parse(time.RFC3339, created)
		out = append(out, t)
	}
	return out, rows.Err()
}

// NextSeq 给新一批任务接着编序号：清空之后重排不该从 0 开始，
// 否则两批任务在界面上会按「新旧混着」的次序跳。
func (s *Store) NextSeq() (int, error) {
	var max *int
	if err := s.db.QueryRow(`SELECT MAX(seq) FROM pub_task`).Scan(&max); err != nil {
		return 0, err
	}
	if max == nil {
		return 0, nil
	}
	return *max + 1, nil
}

func (s *Store) DeleteTask(id string) error {
	_, err := s.db.Exec(`DELETE FROM pub_task WHERE id = ?`, id)
	return err
}

// ClearTasks 只清指定状态的：默认清 todo，
// 已完成的本来就不该继续占着列表，但留着它们不影响「今天的任务」。
func (s *Store) ClearTasks(status string) error {
	if status == "" {
		_, err := s.db.Exec(`DELETE FROM pub_task`)
		return err
	}
	_, err := s.db.Exec(`DELETE FROM pub_task WHERE status = ?`, status)
	return err
}
