package accountsync

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func local() Account {
	return Account{
		Code:      "4A7F-19C2-8E30-5B61",
		Name:      "本地名字",
		Phone:     "13900000000",
		UpdatedAt: time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC),
	}
}

// 没配地址：一个请求都不该发出去。
func TestSyncDisabledMakesNoRequest(t *testing.T) {
	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
	}))
	defer srv.Close()

	// 故意给一个能连上的地址，但把 BaseURL 留空。
	out := Client{HTTP: srv.Client()}.Sync(context.Background(), local())
	if hit {
		t.Fatal("没配地址却发了请求")
	}
	if out.Action != ActionSkipped {
		t.Fatalf("Action = %q，应该是 skipped", out.Action)
	}
	if out.Account != local() {
		t.Fatalf("本地账号被改了：%+v", out.Account)
	}
}

// 服务端比本地新：拉回来。
func TestSyncPullsNewerRemote(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/account/sync" {
			t.Errorf("路径 = %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("没带 token：%q", r.Header.Get("Authorization"))
		}
		var in syncReq
		json.NewDecoder(r.Body).Decode(&in)
		if in.Code != "4A7F-19C2-8E30-5B61" {
			t.Errorf("机器码没传对：%q", in.Code)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"account": map[string]any{
				"code":      in.Code,
				"name":      "服务端的名字",
				"phone":     in.Phone,
				"updatedAt": "2026-11-16T12:00:00Z", // 比本地晚两小时
			},
		})
	}))
	defer srv.Close()

	out := Client{BaseURL: srv.URL, Token: "tok", HTTP: srv.Client()}.
		Sync(context.Background(), local())
	if out.Err != nil {
		t.Fatalf("不该报错：%v", out.Err)
	}
	if out.Action != ActionPulled {
		t.Fatalf("Action = %q，应该是 pulled", out.Action)
	}
	if out.Account.Name != "服务端的名字" {
		t.Fatalf("没拉回服务端的名字：%q", out.Account.Name)
	}
}

// 本地比服务端新：推上去了，但不能被服务端的旧值盖回来。
// 这是最容易写反的一条——用户刚改完名字，一保存就被"同步"回旧名字。
func TestSyncKeepsLocalWhenLocalNewer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"account": map[string]any{
				"name":      "服务端的旧名字",
				"updatedAt": "2026-11-16T08:00:00Z", // 比本地早
			},
		})
	}))
	defer srv.Close()

	out := Client{BaseURL: srv.URL, HTTP: srv.Client()}.Sync(context.Background(), local())
	if out.Action != ActionPushed {
		t.Fatalf("Action = %q，应该是 pushed", out.Action)
	}
	if out.Account.Name != "本地名字" {
		t.Fatalf("本地刚填的被覆盖了：%q", out.Account.Name)
	}
}

// 服务端还没实现这个接口：安静跳过，不能当成配置错误。
func TestSyncTreatsMissingEndpointAsSkipped(t *testing.T) {
	for _, code := range []int{404, 501} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(code)
		}))
		out := Client{BaseURL: srv.URL, HTTP: srv.Client()}.Sync(context.Background(), local())
		srv.Close()
		if out.Err != nil {
			t.Fatalf("%d 被当成错误了：%v", code, out.Err)
		}
		if out.Action != ActionSkipped {
			t.Fatalf("%d 的 Action = %q，应该是 skipped", code, out.Action)
		}
	}
}

// 服务端出错：本地资料必须原样保留。
func TestSyncKeepsLocalOnServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	out := Client{BaseURL: srv.URL, HTTP: srv.Client()}.Sync(context.Background(), local())
	if out.Err == nil {
		t.Fatal("500 应该报错")
	}
	if out.Account != local() {
		t.Fatalf("本地被改了：%+v", out.Account)
	}
}

// 连不上（比如地址写错）：报错但不动本地。
func TestSyncNetworkFailureKeepsLocal(t *testing.T) {
	out := Client{BaseURL: "http://127.0.0.1:1", Timeout: 300 * time.Millisecond}.
		Sync(context.Background(), local())
	if out.Err == nil {
		t.Fatal("连不上应该报错")
	}
	if out.Account.Name != "本地名字" {
		t.Fatalf("本地被改了：%+v", out.Account)
	}
}

// 服务端返回空 account（只登记）：算已同步，不改资料。
func TestSyncEmptyAccountMeansSame(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	out := Client{BaseURL: srv.URL, HTTP: srv.Client()}.Sync(context.Background(), local())
	if out.Action != ActionSame {
		t.Fatalf("Action = %q，应该是 same", out.Action)
	}
	if out.Account != local() {
		t.Fatalf("本地被改了：%+v", out.Account)
	}
}

// 服务端返回的 code 为空时，不能把本地机器码弄丢——
// 丢了等于账号失去身份，下次建档会生成新码，历史全断。
func TestSyncKeepsCodeWhenRemoteOmitsIt(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"account": map[string]any{"name": "新名字", "updatedAt": "2026-11-16T12:00:00Z"},
		})
	}))
	defer srv.Close()

	out := Client{BaseURL: srv.URL, HTTP: srv.Client()}.Sync(context.Background(), local())
	if out.Account.Code != "4A7F-19C2-8E30-5B61" {
		t.Fatalf("机器码丢了：%q", out.Account.Code)
	}
}

// 地址末尾带斜杠不能拼出 //account/sync。
func TestSyncTrimsTrailingSlash(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Path
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	Client{BaseURL: srv.URL + "///", HTTP: srv.Client()}.Sync(context.Background(), local())
	if got != "/account/sync" {
		t.Fatalf("路径 = %q", got)
	}
	if strings.Contains(got, "//") {
		t.Fatalf("路径里有多余斜杠：%q", got)
	}
}
