package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Account 是本机的使用人。
//
// 单行。ID 就是机器码：注册在第一次打开时自动完成，用户不需要知道这件事，
// 他看到的是「名字」和「手机号」两个可以改的字段。
type Account struct {
	ID        string // 机器码
	Name      string
	Phone     string
	CreatedAt time.Time
	UpdatedAt time.Time
	// SyncedAt 空表示还没和服务端同步过。现在恒为空，接服务端时才写。
	SyncedAt string
}

// Account 读本机账号。没有就返回 sql.ErrNoRows，由上层决定要不要建档。
//
// 不用「没有就返回空结构」：空结构会让调用方分不清
// 「还没注册」和「注册了但没填名字」，而这两件事的后续动作不一样。
func (s *Store) Account() (Account, error) {
	var a Account
	var created, updated string
	err := s.db.QueryRow(`SELECT id, name, phone, created_at, updated_at, synced_at
		FROM account LIMIT 1`).
		Scan(&a.ID, &a.Name, &a.Phone, &created, &updated, &a.SyncedAt)
	if err == sql.ErrNoRows {
		return Account{}, sql.ErrNoRows
	}
	if err != nil {
		return Account{}, err
	}
	a.CreatedAt, _ = time.Parse(time.RFC3339, created)
	a.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
	return a, nil
}

// CreateAccount 建一行账号。已存在则什么都不做。
//
// 用 INSERT OR IGNORE 而不是先查后插：两个入口（启动检查、账号页）
// 同时触发建档时，先查后插会撞主键。
func (s *Store) CreateAccount(id string, now time.Time) (Account, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Account{}, fmt.Errorf("机器码为空，无法建档")
	}
	if _, err := s.db.Exec(`INSERT OR IGNORE INTO account
		(id, name, phone, created_at, updated_at) VALUES (?,?,?,?,?)`,
		id, "", "", now.Format(time.RFC3339), now.Format(time.RFC3339)); err != nil {
		return Account{}, fmt.Errorf("建档失败: %w", err)
	}
	return s.Account()
}

// ApplyAccountSync 把一次同步的结果写回本机。
//
// name / phone / updatedAt 可能就是库里已有的值（同步未产生变更时），
// 一条 SQL 一起写，避免「只写了 synced_at、资料却忘写」这种半更新。
//
// 分开两个方法（PullAccount / MarkAccountSynced）看着更"清晰"，但会让调用方
// 自己决定「这两个要不要一起写」——那种决定迟早有人做错，而错了的表现是
// 「同步时间已更新、名字还是旧的」，从界面上完全看不出问题。
func (s *Store) ApplyAccountSync(id, name, phone string, updatedAt time.Time, syncedAt string) error {
	res, err := s.db.Exec(`UPDATE account SET name = ?, phone = ?, updated_at = ?, synced_at = ?
		WHERE id = ?`, name, phone, updatedAt.Format(time.RFC3339), syncedAt, id)
	if err != nil {
		return fmt.Errorf("写回同步结果失败: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("找不到本机账号，重启一次试试")
	}
	return nil
}

// SaveAccountProfile 只改姓名和手机号。机器码不在这里，它是身份不是资料。
func (s *Store) SaveAccountProfile(id, name, phone string, now time.Time) error {
	res, err := s.db.Exec(`UPDATE account SET name = ?, phone = ?, updated_at = ?
		WHERE id = ?`, name, phone, now.Format(time.RFC3339), id)
	if err != nil {
		return fmt.Errorf("保存资料失败: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("找不到本机账号，重启一次试试")
	}
	return nil
}
