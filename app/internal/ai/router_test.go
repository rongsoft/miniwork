package ai

import (
	"context"
	"strings"
	"testing"
)

type fakeClient struct{ name string }

func (f fakeClient) Name() string { return f.name }
func (f fakeClient) Complete(ctx context.Context, system, user string) (string, error) {
	return f.name + ":plain", nil
}

// fakeCreative 实现 Creative，用来验证「写稿真的走了另一个模型」。
type fakeCreative struct{ fakeClient }

func (f fakeCreative) CompleteCreative(ctx context.Context, system, user string) (string, error) {
	return f.name + ":creative", nil
}

func TestRouterSplitsScoreAndCraft(t *testing.T) {
	cheap := fakeClient{"cheap"}
	strong := fakeCreative{fakeClient{"strong"}}
	r := NewRouter(cheap, "cheap").Route(SceneCraft, "strong", strong)

	got, err := r.Complete(context.Background(), "s", "u")
	if err != nil {
		t.Fatal(err)
	}
	if got != "cheap:plain" {
		t.Errorf("打分该走便宜模型，实际 %q", got)
	}

	got, err = r.CompleteCreative(context.Background(), "s", "u")
	if err != nil {
		t.Fatal(err)
	}
	if got != "strong:creative" {
		t.Errorf("写稿该走强模型，实际 %q", got)
	}
}

func TestRouterFallsBackWhenClientNotCreative(t *testing.T) {
	// 写稿通道落到一个只实现 Complete 的客户端上，必须退回普通调用，
	// 而不是报错——功能可以保守，不能是坏的。
	r := NewRouter(fakeClient{"only-plain"}, "only-plain")
	got, err := r.CompleteCreative(context.Background(), "s", "u")
	if err != nil {
		t.Fatal(err)
	}
	if got != "only-plain:plain" {
		t.Errorf("应退回普通通道，实际 %q", got)
	}
}

func TestRouterNameShowsBothModels(t *testing.T) {
	r := NewRouter(fakeClient{"m-small"}, "m-small").Route(SceneCraft, "m-big", fakeClient{"m-big"})
	name := r.Name()
	// 界面上要能一眼看出「写稿用的不是打分那个」。
	if !strings.Contains(name, "m-small") || !strings.Contains(name, "m-big") {
		t.Errorf("分工时名字要带两边，实际 %q", name)
	}
	if strings.Contains(name, "byok") {
		t.Errorf("不该出现模式名，实际 %q", name)
	}
}

func TestRouterNoSplitReportsSingleName(t *testing.T) {
	same := fakeClient{"solo"}
	r := NewRouter(same, "solo").Route(SceneCraft, "solo", same)
	if r.Split() {
		t.Fatal("只配了一个模型不该算分工")
	}
	if r.Name() != "solo" {
		t.Errorf("不分工时名字就是模型名，实际 %q", r.Name())
	}
}

func TestBuildStackSingleModelIsNotRouter(t *testing.T) {
	c := BuildStack("byok", "", "", nil, nil, ModelSpec{
		BaseURL: "https://api.deepseek.com/v1", APIKey: "k", Model: "deepseek-chat",
	})
	if c == nil {
		t.Fatal("单模型配置应当可用")
	}
	// 包一层 Router 没有好处，只会让日志里多一个没意义的名字。
	if _, ok := c.(*Router); ok {
		t.Fatal("单模型不该被包成 Router")
	}
	if ModelNameOf(c) != "deepseek-chat" {
		t.Errorf("应报出真实模型名，实际 %q", ModelNameOf(c))
	}
}

func TestBuildStackRoutesByOrder(t *testing.T) {
	models := []ModelSpec{
		{ID: "cheap", BaseURL: "https://a/v1", APIKey: "k1", Model: "m-cheap"},
		{ID: "strong", BaseURL: "https://b/v1", APIKey: "k2", Model: "m-strong"},
	}
	c := BuildStack("byok", "", "", models, nil, ModelSpec{})
	r, ok := c.(*Router)
	if !ok {
		t.Fatalf("两个模型应组成分工路由，实际 %T", c)
	}
	// 没写 routes 时的默认：第一个打分、最后一个写稿。
	if got := r.SceneID(SceneScore); got != "cheap" {
		t.Errorf("打分默认用第一个，实际 %q", got)
	}
	if got := r.SceneID(SceneCraft); got != "strong" {
		t.Errorf("写稿默认用最后一个，实际 %q", got)
	}
	if r.SceneModel(SceneCraft) != "m-strong" {
		t.Errorf("界面要看到上游模型名，实际 %q", r.SceneModel(SceneCraft))
	}
}

func TestBuildStackHonoursExplicitRoutes(t *testing.T) {
	models := []ModelSpec{
		{ID: "cheap", BaseURL: "https://a/v1", APIKey: "k1", Model: "m-cheap"},
		{ID: "strong", BaseURL: "https://b/v1", APIKey: "k2", Model: "m-strong"},
	}
	// 反过来指定：打分用强的，写稿用便宜的。虽然少见，但配置说什么就是什么。
	c := BuildStack("byok", "", "", models, map[string]string{
		SceneScore: "strong", SceneCraft: "cheap",
	}, ModelSpec{})
	r := c.(*Router)
	if got := r.SceneID(SceneScore); got != "strong" {
		t.Errorf("显式 route 没生效，实际 %q", got)
	}
	if got := r.SceneID(SceneCraft); got != "cheap" {
		t.Errorf("显式 route 没生效，实际 %q", got)
	}
}

func TestBuildStackIgnoresUnknownRouteID(t *testing.T) {
	models := []ModelSpec{
		{ID: "cheap", BaseURL: "https://a/v1", APIKey: "k1", Model: "m-cheap"},
		{ID: "strong", BaseURL: "https://b/v1", APIKey: "k2", Model: "m-strong"},
	}
	c := BuildStack("byok", "", "", models, map[string]string{
		SceneCraft: "not-exist",
	}, ModelSpec{})
	r := c.(*Router)
	// 指向不存在的 id 时不能崩、也不能静默变成「没模型」，
	// 应当退回默认分工。
	if got := r.SceneID(SceneCraft); got != "strong" {
		t.Errorf("未知 route 应退回默认，实际 %q", got)
	}
}

func TestBuildStackDropsIncompleteModels(t *testing.T) {
	models := []ModelSpec{
		{ID: "broken", BaseURL: "", APIKey: "", Model: "m"},
		{ID: "ok", BaseURL: "https://a/v1", APIKey: "k", Model: "m-ok"},
	}
	c := BuildStack("byok", "", "", models, nil, ModelSpec{})
	if c == nil {
		t.Fatal("至少还剩一个可用模型")
	}
	// 只剩一个可用时不该假装在分工。
	if _, ok := c.(*Router); ok {
		t.Fatal("只剩一个模型时不该包成 Router")
	}
}

func TestBuildStackAllBrokenReturnsNil(t *testing.T) {
	models := []ModelSpec{{ID: "broken", Model: "m"}}
	if c := BuildStack("byok", "", "", models, nil, ModelSpec{}); c != nil {
		t.Fatalf("全都配不完整时应返回 nil（退回规则），实际 %T", c)
	}
}

func TestBuildStackRelayModeStillWorks(t *testing.T) {
	c := BuildStack("relay", "https://api.example.com/mw", "tok", nil, nil, ModelSpec{})
	if c == nil {
		t.Fatal("relay 模式应当可用")
	}
	if ModelNameOf(c) != "relay" {
		t.Errorf("relay 没有上游模型名，应退回模式名，实际 %q", ModelNameOf(c))
	}
}

func TestParseSceneRejectsUnknown(t *testing.T) {
	if _, ok := ParseScene("scroe"); ok {
		t.Fatal("拼错的场景名不该被接受，否则会产生一条静默失效的配置")
	}
	if s, ok := ParseScene(" Craft "); !ok || s != SceneCraft {
		t.Fatalf("应容忍大小写与空格，实际 %q %v", s, ok)
	}
}
