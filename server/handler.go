package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type apiResponse struct {
	Stale       bool      `json:"stale"`
	GeneratedAt string    `json:"generatedAt"`
	Items       []Hotspot `json:"items"`
}

// hotspotsHandler 实现数据接口约定里的 GET /data/hotspots。
//
// 契约要点：
//   - niche 必填，中文原样传；关键词搜索时可不传
//   - limit 默认 60
//   - 401/403 → 密钥无效；402/429 → 配额用完
//   - id 稳定、title 必须有
//   - stale 标记上游数据是否过期
func (s *store) hotspotsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	switch s.authorize(r) {
	case authBadToken:
		writeErr(w, http.StatusUnauthorized, "invalid or expired token")
		return
	case authQuota:
		writeErr(w, http.StatusTooManyRequests, "daily quota exceeded")
		return
	}

	q := r.URL.Query()
	niche := strings.TrimSpace(q.Get("niche"))
	keyword := strings.TrimSpace(q.Get("keyword"))
	if niche == "" && keyword == "" {
		writeErr(w, http.StatusBadRequest, "niche or keyword is required")
		return
	}

	limit := 60
	if raw := q.Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > 200 {
		limit = 200
	}

	var since time.Time
	if raw := strings.TrimSpace(q.Get("since")); raw != "" {
		if t, err := time.Parse(time.RFC3339, raw); err == nil {
			since = t
		} else {
			log.Printf("忽略不合法的 since=%q: %v", raw, err)
		}
	}

	snap := s.current()
	var items []Hotspot
	if keyword != "" {
		// 关键词跨赛道搜索：客户端 remote.go 会传 keyword，服务端必须支持，
		// 否则「获取灵感」在接了数据源之后反而不能用。
		items = searchAll(snap, keyword, limit)
	} else {
		items = snap.Items[niche]
		if items == nil {
			// 未知赛道返回空列表，而不是报错——客户端会显示「没有相关内容」，
			// 比弹一个服务端错误更可懂。
			items = []Hotspot{}
		}
	}

	// since 过滤：客户端目前不传，留着给增量同步。
	if !since.IsZero() {
		filtered := make([]Hotspot, 0, len(items))
		for _, it := range items {
			pub, err := time.Parse(time.RFC3339, it.PublishedAt)
			if err == nil && !pub.Before(since) {
				filtered = append(filtered, it)
			}
		}
		items = filtered
	}
	if len(items) > limit {
		items = items[:limit]
	}

	writeJSON(w, http.StatusOK, apiResponse{
		Stale:       snap.Stale,
		GeneratedAt: snap.GeneratedAt,
		Items:       items,
	})
}

// searchAll 在全部赛道里按标题子串找，按热度降序。
func searchAll(snap snapshot, keyword string, limit int) []Hotspot {
	kw := strings.ToLower(keyword)
	out := make([]Hotspot, 0, 32)
	seen := map[string]bool{}
	for _, niche := range nicheOrder {
		for _, it := range snap.Items[niche] {
			if seen[it.ID] || !strings.Contains(strings.ToLower(it.Title), kw) {
				continue
			}
			seen[it.ID] = true
			out = append(out, it)
		}
	}
	// 简单插入排序即可：结果集最多几百条，没必要引 sort 之外的东西。
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].Heat > out[j-1].Heat; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("写响应失败: %v", err)
	}
}

// healthHandler 给部署后探活用，也方便我本地确认服务真的起来了。
func (s *store) healthHandler(w http.ResponseWriter, r *http.Request) {
	snap := s.current()
	counts := map[string]int{}
	for niche, items := range snap.Items {
		counts[niche] = len(items)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":          true,
		"generatedAt": snap.GeneratedAt,
		"counts":      counts,
		"version":     fmt.Sprintf("miniwork-server %s built with %s", "0.1.0", "go"),
	})
}

// refreshHandler 手动触发刷新，本地测试「刷新有新意」用，生产可关掉。
func (s *store) refreshHandler(w http.ResponseWriter, r *http.Request) {
	s.refresh()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "generatedAt": s.current().GeneratedAt})
}
