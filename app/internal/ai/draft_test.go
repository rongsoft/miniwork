package ai

import (
	"strings"
	"testing"
)

func TestParseDraftPlainJSON(t *testing.T) {
	raw := `{"title":"我把简历投了 40 次","body":"第一段\n\n第二段","tags":["职场","求职"],"note":"从失败次数切入"}`
	d, err := ParseDraft(raw)
	if err != nil {
		t.Fatal(err)
	}
	if d.Title != "我把简历投了 40 次" || !strings.Contains(d.Body, "第二段") {
		t.Fatalf("解析结果不对: %+v", d)
	}
	if len(d.Tags) != 2 {
		t.Fatalf("标签应有 2 个，实际 %v", d.Tags)
	}
}

func TestParseDraftToleratesChatterAndCodeFence(t *testing.T) {
	raw := "好的，我来写这篇：\n```json\n{\"title\":\"标题\",\"body\":\"正文\"}\n```\n希望有用。"
	d, err := ParseDraft(raw)
	if err != nil {
		t.Fatal(err)
	}
	if d.Title != "标题" || d.Body != "正文" {
		t.Fatalf("应能容忍模型加话，实际 %+v", d)
	}
}

func TestParseDraftBuildsBodyFromSegments(t *testing.T) {
	// 模型有时只把台词写进分镜，body 给空。直接报错等于让用户白等一次。
	raw := `{"title":"视频","segments":[{"at":"0-3s","shot":"拍桌面","line":"第一句"},{"at":"3-10s","shot":"拍人","line":"第二句"}]}`
	d, err := ParseDraft(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(d.Body, "第一句") || !strings.Contains(d.Body, "第二句") {
		t.Fatalf("应从分镜拼出台词，实际 %q", d.Body)
	}
}

func TestParseDraftAcceptsCommaSeparatedTags(t *testing.T) {
	raw := `{"title":"标题","body":"正文","tags":"#职场, 副业、求职"}`
	d, err := ParseDraft(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Tags) != 3 {
		t.Fatalf("应拆成 3 个标签，实际 %v", d.Tags)
	}
	for _, tag := range d.Tags {
		if strings.HasPrefix(tag, "#") {
			t.Errorf("标签不该保留井号: %q", tag)
		}
	}
}

func TestParseDraftFallsBackToFirstLine(t *testing.T) {
	raw := `{"body":"第一句就是标题\n第二段"}`
	d, err := ParseDraft(raw)
	if err != nil {
		t.Fatal(err)
	}
	if d.Title != "第一句就是标题" {
		t.Fatalf("没有标题时应取正文首行，实际 %q", d.Title)
	}
}

func TestParseDraftRejectsEmptyBody(t *testing.T) {
	if _, err := ParseDraft(`{"title":"只有标题"}`); err == nil {
		t.Fatal("正文为空应当报错，否则用户会得到一篇空稿")
	}
}

func TestBuildDraftPromptCarriesPlatformLimits(t *testing.T) {
	_, _ = BuildDraftPrompt(DraftInput{
		Topic: "被裁员之后", Platform: "小红书", Format: FormatLong,
		TitleMax: 20, BodyMax: 1000, TagMax: 10,
	}, Brief{Niche: "职场"})

	sys := longDraftSystem(DraftInput{TitleMax: 20, BodyMax: 1000, TagMax: 10})
	if !strings.Contains(sys, "1000") || !strings.Contains(sys, "20 字") {
		t.Fatalf("长文提示词要带上平台上限，实际:\n%s", sys)
	}
	// 这几条是防止模型写成提纲、或者一句一行的关键约束，改提示词时别顺手删掉。
	for _, must := range []string{"不许编", "段落要成段", "同一段里连着两句都从「你」开始", "你怎么看"} {
		if !strings.Contains(sys, must) {
			t.Errorf("长文提示词缺少硬约束: %q", must)
		}
	}
}

func TestBuildShortPromptAsksForSegments(t *testing.T) {
	sys := shortDraftSystem(DraftInput{TitleMax: 55, TagMax: 5})
	for _, must := range []string{"segments", "260-380", "4-6 个镜头", "点赞关注"} {
		if !strings.Contains(sys, must) {
			t.Errorf("口播提示词缺少关键约束: %q", must)
		}
	}
}

func TestShortPromptUsesShortFormat(t *testing.T) {
	in := DraftInput{Topic: "被裁员", Platform: "抖音", Format: FormatShort}
	sys, _ := BuildDraftPrompt(in, Brief{Niche: "职场"})
	if !strings.Contains(sys, "短视频口播稿写手") {
		t.Fatal("口播形态必须走口播提示词")
	}
}

func TestPromptSaysNoMaterialWhenEmpty(t *testing.T) {
	_, user := BuildDraftPrompt(DraftInput{Topic: "x", Platform: "小红书"}, Brief{})
	// 没有素材时必须明说，否则模型会自己造一个「我朋友」。
	if !strings.Contains(user, "没提供个人素材") {
		t.Fatalf("缺素材时应明确禁止编造，实际:\n%s", user)
	}
}

func TestPromptIncludesSkeleton(t *testing.T) {
	_, user := BuildDraftPrompt(DraftInput{
		Topic: "x", Platform: "小红书",
		Skeleton: []string{"先讲结果", "再讲代价"},
	}, Brief{})
	if !strings.Contains(user, "1. 先讲结果") || !strings.Contains(user, "2. 再讲代价") {
		t.Fatalf("拆解骨架要带进提示词，实际:\n%s", user)
	}
}
