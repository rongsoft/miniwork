package ai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBuildPromptCarriesMemory(t *testing.T) {
	b := Brief{
		Niche:       "职场",
		Positioning: "二线城市HR，专写职场潜规则",
		Audience:    "工作 3-8 年的白领",
		Tone:        "克制、不说教",
		Avoid:       "不碰裁员赔偿的法律细节",
		Picked:      []string{"35岁裁员后我把副业做成主业"},
		Dropped:     []string{"新款手机发布"},
	}
	system, user := BuildPrompt(b, []Item{
		{ID: "x1", Platform: "xiaohongshu", Title: "领导突然客气", Heat: 74000, AgeLabel: "8 小时前"},
	})

	// 人设必须进提示词，否则「长期记忆」等于没做。
	for _, want := range []string{"二线城市HR", "工作 3-8 年的白领", "克制、不说教", "不碰裁员赔偿"} {
		if !strings.Contains(user, want) {
			t.Fatalf("提示词缺少人设「%s」:\n%s", want, user)
		}
	}
	if !strings.Contains(user, "收下过的") || !strings.Contains(user, "丢掉过的") {
		t.Fatalf("提示词缺少历史决策:\n%s", user)
	}
	if !strings.Contains(user, "id=x1 | 小红书 | 7.4万互动 | 8 小时前 | 领导突然客气") {
		t.Fatalf("候选行格式不对:\n%s", user)
	}

	// 系统提示词里三条硬约束不能丢，它们是理由质量的全部来源。
	for _, want := range []string{"跑题的爆款也必须给低分", "禁止复述", "区分度"} {
		if !strings.Contains(system, want) {
			t.Fatalf("系统提示词缺少约束「%s」", want)
		}
	}
}

func TestBuildPromptSaysWhenProfileEmpty(t *testing.T) {
	_, user := BuildPrompt(Brief{Niche: "职场"}, []Item{{ID: "a", Title: "t"}})
	// 空人设时必须明说「别假设他的身份」，否则模型会自己编一个 HR 出来。
	if !strings.Contains(user, "不要假设他的身份") {
		t.Fatalf("空人设缺少提示:\n%s", user)
	}
}

func TestBuildPromptCapsMemory(t *testing.T) {
	var many []string
	for i := 0; i < 30; i++ {
		many = append(many, "标题"+strings.Repeat("x", i+1))
	}
	_, user := BuildPrompt(Brief{Niche: "职场", Picked: many}, []Item{{ID: "a", Title: "t"}})
	if n := strings.Count(user, "- 标题"); n != memoryCap {
		t.Fatalf("历史最多带 %d 条，实际 %d", memoryCap, n)
	}
}

func TestParseTolerant(t *testing.T) {
	content := "好的，结果如下：\n```json\n[" +
		`{"id":"a","fit":88,"reason":"场景具体，能落到他自己的经历上"},` +
		`{"id":"b","fit":"72分","reason":"  角度还行  "},` +
		`{"id":"c","fit":120,"reason":"超范围"},` +
		`{"id":"","fit":50,"reason":"没有 id"},` +
		`{"id":3,"fit":40,"reason":"数字 id"}` +
		"]\n```\n希望有帮助。"

	got, err := Parse(content)
	if err != nil {
		t.Fatalf("应能容忍代码块与前后废话: %v", err)
	}
	if got["a"].Fit != 88 || got["a"].Reason == "" {
		t.Fatalf("正常项解析失败: %+v", got["a"])
	}
	if got["b"].Fit != 72 || got["b"].Reason != "角度还行" {
		t.Fatalf("字符串分数与空白应被规整: %+v", got["b"])
	}
	if got["c"].Fit != 100 {
		t.Fatalf("超范围应封顶 100，实际 %d", got["c"].Fit)
	}
	if _, ok := got[""]; ok {
		t.Fatal("空 id 必须丢掉")
	}
	if got["3"].Fit != 40 {
		t.Fatalf("数字 id 应转成字符串，实际 %+v", got)
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	if _, err := Parse("抱歉，我无法完成这个请求。"); err == nil {
		t.Fatal("没有数组时必须报错，否则会被当成 0 条静默通过")
	}
	if _, err := Parse("[]"); err == nil {
		t.Fatal("空数组应报错")
	}
}

func TestParseClipsLongReason(t *testing.T) {
	long := strings.Repeat("很", 80)
	got, err := Parse(`[{"id":"a","fit":60,"reason":"` + long + `"}]`)
	if err != nil {
		t.Fatal(err)
	}
	if n := len([]rune(got["a"].Reason)); n > 40 {
		t.Fatalf("理由应截断到 40 字，实际 %d", n)
	}
}

func TestRelaySendsSceneNotModel(t *testing.T) {
	var body map[string]any
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ai/relay" {
			t.Errorf("路径应为 /ai/relay，实际 %s", r.URL.Path)
		}
		auth = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"[{\"id\":\"a\",\"fit\":80,\"reason\":\"x\"}]"}}]}`))
	}))
	defer srv.Close()

	out, err := NewRelay(srv.URL, "lic-1").Complete(context.Background(), "sys", "usr")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"fit":80`) {
		t.Fatalf("正文没取对: %s", out)
	}
	// 客户端只传 scene：换模型不用发版，就靠这一条。
	if body["scene"] != SceneScore {
		t.Fatalf("必须传 scene，实际 %v", body["scene"])
	}
	if _, ok := body["model"]; ok {
		t.Fatal("relay 模式不该传 model")
	}
	if auth != "Bearer lic-1" {
		t.Fatalf("鉴权头不对: %q", auth)
	}
}

func TestByokSendsModel(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("路径应为 /chat/completions，实际 %s", r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer srv.Close()

	if _, err := NewByok(srv.URL, "sk-1", "my-model").Complete(context.Background(), "s", "u"); err != nil {
		t.Fatal(err)
	}
	if body["model"] != "my-model" {
		t.Fatalf("byok 必须传 model，实际 %v", body["model"])
	}
	if _, ok := body["scene"]; ok {
		t.Fatal("byok 不该传 scene")
	}
}

func TestStatusMapping(t *testing.T) {
	cases := []struct {
		code int
		want error
	}{
		{http.StatusPaymentRequired, ErrQuotaExceeded},
		{http.StatusTooManyRequests, ErrQuotaExceeded},
		{http.StatusUnauthorized, ErrUnauthorized},
		{http.StatusForbidden, ErrUnauthorized},
	}
	for _, c := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(c.code)
			_, _ = w.Write([]byte(`{"error":{"message":"nope"}}`))
		}))
		_, err := NewRelay(srv.URL, "").Complete(context.Background(), "s", "u")
		srv.Close()
		if !errors.Is(err, c.want) {
			t.Fatalf("%d 应映射为 %v，实际 %v", c.code, c.want, err)
		}
	}
}

func TestNewHonoursMode(t *testing.T) {
	if New("off", "", "", "", "", "") != nil {
		t.Fatal("off 应返回 nil")
	}
	if New("relay", "", "tok", "", "", "") != nil {
		t.Fatal("relay 缺地址应返回 nil，而不是一个必然失败的客户端")
	}
	if New("relay", "https://x", "tok", "", "", "") == nil {
		t.Fatal("relay 配齐应返回客户端")
	}
	if New("byok", "", "", "https://x", "", "") != nil {
		t.Fatal("byok 缺 key 应返回 nil")
	}
	if New("byok", "", "", "https://x", "sk", "m") == nil {
		t.Fatal("byok 配齐应返回客户端")
	}
}

func TestTimeoutIsBounded(t *testing.T) {
	if requestTimeout > time.Minute {
		t.Fatalf("超时太久会让首启动卡死，实际 %v", requestTimeout)
	}
}
