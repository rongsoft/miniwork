package pipeline

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"rongsoft/miniwork/app/internal/source"
	"rongsoft/miniwork/app/internal/store"
)

// deadSource 模拟数据源挂掉。桌面端最常遇到的真实故障就是它。
type deadSource struct{ err error }

func (d deadSource) Name() string { return "remote" }
func (d deadSource) Fetch(context.Context, source.Query) ([]source.Hotspot, error) {
	return nil, d.err
}

func newStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatalf("打开库失败: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func seedCache(t *testing.T, st *store.Store, niche string) {
	t.Helper()
	now := time.Now()
	list := []source.Hotspot{
		{ID: "c1", Platform: "xiaohongshu", Title: "35岁裁员后，我用3个月把副业做成主业", Heat: 180000, PublishedAt: now.Add(-2 * time.Hour), FetchedAt: now},
		{ID: "c2", Platform: "douyin", Title: "为什么跳槽涨薪比内部晋升快？", Heat: 96000, PublishedAt: now.Add(-5 * time.Hour), FetchedAt: now},
	}
	if _, err := st.SaveHotspots(list, niche); err != nil {
		t.Fatalf("写缓存失败: %v", err)
	}
}

func TestFetchFallsBackToCache(t *testing.T) {
	st := newStore(t)
	seedCache(t, st, "职场")

	src := deadSource{err: errors.New("dial tcp: timeout")}
	res, err := Run(context.Background(), st, src, nil, Input{Niche: "职场", Limit: 10, MinScore: 50}, nil)
	if err != nil {
		t.Fatalf("有缓存时不该整体失败: %v", err)
	}
	if !res.Stale {
		t.Fatal("用缓存必须标 stale=true，否则界面会让用户误以为数据是刚取的")
	}
	if res.Source != "cache" {
		t.Fatalf("数据来源应标为 cache，实际 %q", res.Source)
	}
	if len(res.Topics) == 0 {
		t.Fatal("缓存里的内容应能照常打分出选题")
	}
}

func TestCacheIsScopedByNiche(t *testing.T) {
	st := newStore(t)
	seedCache(t, st, "职场")

	// 换成理财赛道：缓存里没有它的内容，就不该拿职场的顶替。
	_, err := Run(context.Background(), st, deadSource{err: errors.New("boom")}, nil,
		Input{Niche: "理财", Limit: 10, MinScore: 50}, nil)
	if err == nil {
		t.Fatal("没有该赛道的缓存时必须报错，不能串赛道")
	}
}

func TestFetchFailsWithoutCache(t *testing.T) {
	st := newStore(t)
	_, err := Run(context.Background(), st, deadSource{err: errors.New("boom")}, nil,
		Input{Niche: "职场", Limit: 10, MinScore: 50}, nil)
	if err == nil {
		t.Fatal("既无数据源又无缓存时，必须明确失败")
	}
	if !strings.Contains(err.Error(), "本地也没有可用的缓存") {
		t.Fatalf("错误信息应指向下一步动作，实际: %v", err)
	}
}

func TestQuotaErrorIsHumanReadable(t *testing.T) {
	st := newStore(t)
	_, err := Run(context.Background(), st, deadSource{err: source.ErrQuotaExceeded}, nil,
		Input{Niche: "职场", Limit: 10, MinScore: 50}, nil)
	if err == nil {
		t.Fatal("配额用完应报错")
	}
	if !strings.Contains(err.Error(), "配额") {
		t.Fatalf("应把底层错误翻成人话，实际: %v", err)
	}
}

func TestStaleCacheDoesNotRefreshTimestamp(t *testing.T) {
	st := newStore(t)
	// 四天前的缓存：超出 72 小时窗口，不该被当成今天的。
	old := time.Now().Add(-96 * time.Hour)
	if _, err := st.SaveHotspots([]source.Hotspot{{
		ID: "old", Platform: "xiaohongshu", Title: "过期的职场内容",
		PublishedAt: old, FetchedAt: old,
	}}, "职场"); err != nil {
		t.Fatal(err)
	}

	_, err := Run(context.Background(), st, deadSource{err: errors.New("boom")}, nil,
		Input{Niche: "职场", Limit: 10, MinScore: 50}, nil)
	if err == nil {
		t.Fatal("超出窗口的缓存不应参与降级")
	}
}
