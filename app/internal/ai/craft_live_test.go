//go:build live

// 联调测试：真的调一次模型。
//
// 单独放在 live 标签下，是因为它要花真钱、要读本机配置、结果还不确定
// （模型每次措辞都不同）。放进默认测试会让 CI 变成玄学。
//
// 用法：
//
//	go test -tags live ./internal/ai/ -v -run Live
package ai

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// liveClient 从用户配置文件里读 byok 设置。
// 读不到就跳过——没有 key 的环境里让测试失败没有意义。
func liveClient(t *testing.T) Client {
	t.Helper()
	dir, err := os.UserConfigDir()
	if err != nil {
		t.Skip("拿不到配置目录")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "miniwork", "config.json"))
	if err != nil {
		t.Skip("没有配置文件，跳过联调")
	}
	var cfg struct {
		AI struct {
			Mode    string `json:"mode"`
			BaseURL string `json:"baseUrl"`
			APIKey  string `json:"apiKey"`
			Model   string `json:"model"`
		} `json:"ai"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Skip("配置读不出来，跳过")
	}
	c := New(cfg.AI.Mode, "", "", cfg.AI.BaseURL, cfg.AI.APIKey, cfg.AI.Model)
	if c == nil {
		t.Skip("模型未配置，跳过联调")
	}
	return c
}

func TestLiveTeardown(t *testing.T) {
	c := liveClient(t)

	brief := Brief{Niche: "职场", Positioning: "帮 30+ 打工人做职业转型", Audience: "被优化过的中层"}
	system, user := BuildTeardownPrompt(
		"35 岁被优化后，我用 40 天找到了更好的工作",
		"去年 11 月被通知优化。我把简历改了三处，投了 17 家，面了 5 家。",
		brief,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	content, err := c.Complete(ctx, system, user)
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	t.Logf("原始返回：\n%s", content)

	td, err := ParseTeardown(content)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if td.Title == "" || td.Tone == "" {
		t.Fatalf("短字段是空的：%+v", td)
	}
	if td.Structure == "" {
		t.Fatalf("内容结构是空的：%+v", td)
	}
	// 这一版的产品底线是「一行」：字段超过 60 字就说明模型又把分析报告搬回来了。
	for name, v := range map[string]string{
		"标题亮点": td.Title, "封面设计": td.Cover, "内容结构": td.Structure,
		"互动引导": td.Engage, "语气风格": td.Tone,
	} {
		if n := len([]rune(v)); n > 60 {
			t.Errorf("%s 太长（%d 字），卡片会被撑成一段文字：%s", name, n, v)
		}
	}
	if td.Learn == "" {
		t.Log("提醒：learn 为空，提示词里要求这是最重要的一行")
	}
	t.Logf("拆解结果：%+v", td)
}

func TestLiveTitles(t *testing.T) {
	c := liveClient(t)

	brief := Brief{Niche: "职场", Positioning: "帮 30+ 打工人做职业转型", Audience: "被优化过的中层"}
	system, user := BuildTitlePrompt(
		"35 岁被优化，我用 40 天找到更好的工作，关键是先想明白三件事",
		[]string{"悬念", "数字", "冲突"},
		brief,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	content, err := c.Complete(ctx, system, user)
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	t.Logf("原始返回：\n%s", content)

	list, err := ParseTitles(content)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(list) < 5 {
		t.Fatalf("应该给 8 个左右，只拿到 %d 个", len(list))
	}

	// 八条不能是八个同义句——这是提示词里最核心的一条要求，必须验。
	seen := map[string]bool{}
	for _, item := range list {
		if seen[item.Text] {
			t.Fatalf("出现了重复标题：%s", item.Text)
		}
		seen[item.Text] = true
		if n := len([]rune(item.Text)); n > 30 {
			t.Fatalf("标题太长（%d 字）：%s", n, item.Text)
		}
	}
	for _, item := range list {
		t.Logf("[%s] %s — %s", item.Hook, item.Text, item.Note)
	}
}
