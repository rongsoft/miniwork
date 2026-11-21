package main

import (
	"testing"
	"time"
)

// 验证随时间放量：同一批内容 id 稳定，但新轮次会露出更多条。
func TestSnapshotGrowsAndKeepsIDsStable(t *testing.T) {
	cfg := Config{RefreshMinutes: 10, BaseItems: 5, StepItems: 3}
	start := time.Date(2026, 11, 20, 9, 0, 0, 0, time.Local)

	s0 := buildSnapshot(cfg, start, start)
	s1 := buildSnapshot(cfg, start, start.Add(10*time.Minute))
	s2 := buildSnapshot(cfg, start, start.Add(20*time.Minute))

	n0, n1, n2 := len(s0.Items["职场"]), len(s1.Items["职场"]), len(s2.Items["职场"])
	if !(n0 < n1 && n1 < n2) {
		t.Fatalf("条数应随时间增长: %d -> %d -> %d", n0, n1, n2)
	}
	// 先出现的 id 必须一直存在（幂等：老内容不会被换掉，只是被新内容顶下去）。
	set1 := map[string]bool{}
	for _, it := range s1.Items["职场"] {
		set1[it.ID] = true
	}
	for _, it := range s0.Items["职场"] {
		if !set1[it.ID] {
			t.Fatalf("老条目 id %s 在新快照里消失了", it.ID)
		}
	}
}

// 验证 id 稳定：同一标题在任何时刻算出的 id 都一样。
func TestStableID(t *testing.T) {
	a := stableID("职场", "xiaohongshu", "测试标题")
	b := stableID("职场", "xiaohongshu", "测试标题")
	c := stableID("职场", "douyin", "测试标题")
	if a != b {
		t.Fatal("同内容 id 必须稳定")
	}
	if a == c {
		t.Fatal("不同平台应算作不同内容 id")
	}
}

// 验证所有 8 个赛道都有内容，且不为空。
func TestAllNichesHaveData(t *testing.T) {
	cfg := Config{RefreshMinutes: 10, BaseItems: 5, StepItems: 3}
	now := time.Now()
	s := buildSnapshot(cfg, now, now)
	for _, niche := range nicheOrder {
		if len(s.Items[niche]) == 0 {
			t.Fatalf("赛道 %q 没有数据", niche)
		}
		for _, it := range s.Items[niche] {
			if it.ID == "" || it.Title == "" {
				t.Fatalf("赛道 %q 存在 id/title 为空的条目", niche)
			}
		}
	}
}

// 验证热度降序：界面今天这批按热度排，服务端顺序也要一致。
func TestItemsSortedByRevealTime(t *testing.T) {
	cfg := Config{RefreshMinutes: 10, BaseItems: 10, StepItems: 3}
	now := time.Now()
	s := buildSnapshot(cfg, now, now)
	items := s.Items["美食"]
	for i := 1; i < len(items); i++ {
		if items[i].PublishedAt > items[i-1].PublishedAt {
			t.Fatalf("未按发布时间降序: %v > %v", items[i].PublishedAt, items[i-1].PublishedAt)
		}
	}
}
