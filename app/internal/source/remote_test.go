package source

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRemoteParsesItems(t *testing.T) {
	var gotNiche, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotNiche = r.URL.Query().Get("niche")
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"stale":false,"items":[
			{"id":"xhs_1","platform":"xiaohongshu","title":"标题一","heat":1200,
			 "tags":["职场","裁员"],"publishedAt":"2026-11-14T08:00:00Z"},
			{"id":"dy_1","platform":"douyin","title":"标题二","heat":300,"published_at":"2026-11-13 20:00:00"},
			{"id":"","platform":"douyin","title":"没有 id"},
			{"id":"xhs_1","platform":"xiaohongshu","title":"重复的 id"},
			{"id":"xhs_2","platform":"xiaohongshu","title":"   "}
		]}`))
	}))
	defer srv.Close()

	src := NewRemote(srv.URL, "k-123")
	list, err := src.Fetch(context.Background(), Query{Niche: "职场", Since: time.Time{}})
	if err != nil {
		t.Fatalf("取数失败: %v", err)
	}
	if gotNiche != "职场" {
		t.Fatalf("赛道应作为查询参数传给服务端，实际 %q", gotNiche)
	}
	if gotAuth != "Bearer k-123" {
		t.Fatalf("密钥应放在 Authorization 头，实际 %q", gotAuth)
	}
	// 缺 id、重复 id、空标题的三条必须被丢掉。
	if len(list) != 2 {
		t.Fatalf("应剩 2 条有效内容，实际 %d: %+v", len(list), list)
	}
	if list[0].Title != "标题一" || list[0].Heat != 1200 {
		t.Fatalf("字段映射不对: %+v", list[0])
	}
	if list[0].FetchedAt.IsZero() {
		t.Fatal("FetchedAt 必须由客户端补齐，否则落库会写进 0001 年")
	}
	// snake_case 的 published_at 也要认。
	if list[1].PublishedAt.Year() != 2026 || list[1].PublishedAt.Hour() != 20 {
		t.Fatalf("snake_case 时间解析失败: %v", list[1].PublishedAt)
	}
}

func TestRemoteAcceptsBareArray(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"id":"a","platform":"douyin","title":"数组形态"}]`))
	}))
	defer srv.Close()

	list, err := NewRemote(srv.URL, "").Fetch(context.Background(), Query{Niche: "职场", Since: time.Time{}})
	if err != nil {
		t.Fatalf("顶层数组也应能解析: %v", err)
	}
	if len(list) != 1 || list[0].Title != "数组形态" {
		t.Fatalf("解析结果不对: %+v", list)
	}
}

func TestRemoteMapsStatusCodes(t *testing.T) {
	cases := []struct {
		code int
		want error
	}{
		{http.StatusUnauthorized, ErrNotConfigured},
		{http.StatusForbidden, ErrNotConfigured},
		{http.StatusPaymentRequired, ErrQuotaExceeded},
		{http.StatusTooManyRequests, ErrQuotaExceeded},
	}
	for _, c := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(c.code)
			_, _ = w.Write([]byte(`{"error":"nope"}`))
		}))
		_, err := NewRemote(srv.URL, "k").Fetch(context.Background(), Query{Niche: "职场", Since: time.Time{}})
		srv.Close()
		if !errors.Is(err, c.want) {
			t.Fatalf("%d 应映射为 %v，实际 %v", c.code, c.want, err)
		}
	}
}

func TestRemoteRejectsBadPayload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html>502 Bad Gateway</html>`))
	}))
	defer srv.Close()

	if _, err := NewRemote(srv.URL, "").Fetch(context.Background(), Query{Niche: "职场", Since: time.Time{}}); err == nil {
		t.Fatal("非 json 响应必须报错，不能当成 0 条静默通过")
	}
}

func TestRemoteWithoutBaseURL(t *testing.T) {
	_, err := NewRemote("", "").Fetch(context.Background(), Query{Niche: "职场", Since: time.Time{}})
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("空地址应返回 ErrNotConfigured，实际 %v", err)
	}
}

func TestBuildFallsBackToMockWhenNoBaseURL(t *testing.T) {
	src, label := Build(Spec{Kind: "remote", BaseURL: ""})
	if src.Name() != "mock" {
		t.Fatalf("没填地址应退回 mock，实际 %s", src.Name())
	}
	// 降级必须写在界面上，否则用户会以为看的是真榜单。
	if label == "" || label == "线上数据" {
		t.Fatalf("降级标签不对: %q", label)
	}

	src2, label2 := Build(Spec{Kind: "remote", BaseURL: "https://api.example.com"})
	if src2.Name() != "remote" || label2 != "线上数据" {
		t.Fatalf("填了地址应走 remote，实际 %s / %s", src2.Name(), label2)
	}
}
