package main

import (
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// store 持有当前快照，并统计配额。
//
// 快照整份替换而不是逐条改：handler 读的时候拿到的是一个不可变整体，
// 不会出现「读了一半被刷新改掉」的撕裂。
type store struct {
	mu      sync.RWMutex
	snap    snapshot
	started time.Time

	day   int32 // 配额计数对应的日期（2006-01-02 转 int）
	quota int32 // 当日已用请求数
	cfg   Config
}

func newStore(cfg Config) *store {
	now := time.Now()
	return &store{
		snap:    buildSnapshot(cfg, now, now),
		started: now,
		cfg:     cfg,
	}
}

// refresh 重建快照。定时器和手动触发都走这里。
func (s *store) refresh() {
	now := time.Now()
	snap := buildSnapshot(s.cfg, s.started, now)
	// 内置数据源每次重建都是「刚刚生成」的，所以 stale 恒为 false。
	// 真实部署换成易撰适配器后，这里改成判断上游 generatedAt 的年龄即可；
	// 客户端那边的 stale 兜底逻辑已经写好，服务端只要如实给这个标志。
	snap.Stale = false
	s.mu.Lock()
	s.snap = snap
	s.mu.Unlock()
}

func (s *store) current() snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snap
}

// takeQuota 按自然日计数。返回 false 表示今日配额用完。
func (s *store) takeQuota() bool {
	if s.cfg.QuotaDay <= 0 {
		return true
	}
	today := int32(dayNumber(time.Now()))
	for {
		old := atomic.LoadInt32(&s.day)
		if old == today {
			break
		}
		if atomic.CompareAndSwapInt32(&s.day, old, today) {
			atomic.StoreInt32(&s.quota, 0)
			break
		}
	}
	for {
		n := atomic.LoadInt32(&s.quota)
		if n >= int32(s.cfg.QuotaDay) {
			return false
		}
		if atomic.CompareAndSwapInt32(&s.quota, n, n+1) {
			return true
		}
	}
}

func dayNumber(t time.Time) int {
	return t.Year()*10000 + int(t.Month())*100 + t.Day()
}

// 鉴权结果。
type authError int

const (
	authOK authError = iota
	authBadToken
	authQuota
)

func (s *store) authorize(r *http.Request) authError {
	if s.cfg.Token != "" {
		h := r.Header.Get("Authorization")
		want := "Bearer " + s.cfg.Token
		if !strings.EqualFold(h, want) {
			return authBadToken
		}
	}
	if !s.takeQuota() {
		return authQuota
	}
	return authOK
}
