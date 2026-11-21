//go:build live

package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"rongsoft/miniwork/app/internal/ai"
	"rongsoft/miniwork/app/internal/config"
	"rongsoft/miniwork/app/internal/store"
	"rongsoft/miniwork/app/internal/topic"
)

// 真实调用一次模型出稿。用 -tags live 才跑，避免常规测试烧额度。
//
//	go test -tags live -run TestLiveWrite -v .
func TestLiveWrite(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("读配置失败: %v", err)
	}
	client := ai.BuildStack(cfg.AI.Mode, cfg.AI.RelayURL, cfg.AI.RelayToken,
		toModelSpecs(cfg.AI.Models), cfg.AI.Routes,
		ai.ModelSpec{BaseURL: cfg.AI.BaseURL, APIKey: cfg.AI.APIKey, Model: cfg.AI.Model})
	if client == nil {
		t.Skip("没有配置模型，跳过")
	}
	t.Logf("模型栈: %s", ai.ModelNameOf(client))

	st, err := store.Open(filepath.Join(t.TempDir(), "live.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	topicID := "t-live"
	if _, err := st.SaveTopic(store.Topic{
		ID: topicID, Title: "35 岁被优化，我用 40 天找到更好的工作",
		Platform: "xiaohongshu", Niche: "职场", Score: 88,
		Status: topic.Draft, CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveProfile(store.Profile{
		Niche: "职场", Positioning: "做了 12 年软件外包，现在做自己的产品",
		Audience: "30-40 岁想转型的技术人", Tone: "直接、不喊口号",
	}); err != nil {
		t.Fatal(err)
	}

	svc := &TopicService{st: st, aiClient: client}

	material := "我在烟台做 web 系统外包 12 年，去年客户少了三成，才开始做自己的产品。" +
		"最难受的不是没钱，是每天打开电脑不知道今天该做什么。"

	start := time.Now()
	long, err := svc.Write(topicID, "xiaohongshu", "long", material, nil)
	if err != nil {
		t.Fatalf("图文稿失败: %v", err)
	}
	t.Logf("图文稿 %.1fs  %d 字  标题 %d 字  标签 %d 个",
		time.Since(start).Seconds(), long.BodyLen, long.TitleLen, len(long.Tags))
	t.Logf("标题: %s", long.Title)
	t.Logf("正文:\n%s", long.Body)
	t.Logf("标签: %s", strings.Join(long.Tags, " / "))
	if len(long.Issues) > 0 {
		for _, is := range long.Issues {
			t.Logf("提醒: %s", is.Text)
		}
	}

	// 硬约束检查：这些都是提示词里明确禁止的，模型犯了就是提示词没写住。
	banned := []string{"大家好", "在这个时代", "最近很多人问我", "今天想聊聊", "首先", "其次", "综上所述", "你怎么看", "**", "##"}
	for _, b := range banned {
		if strings.Contains(long.Body, b) {
			t.Errorf("正文出现了被禁的表达: %q", b)
		}
	}
	if long.BodyLen > long.BodyMax {
		t.Errorf("正文 %d 字超过平台上限 %d", long.BodyLen, long.BodyMax)
	}

	start = time.Now()
	short, err := svc.Write(topicID, "douyin", "short", material, long.Tags)
	if err != nil {
		t.Fatalf("口播稿失败: %v", err)
	}
	t.Logf("口播稿 %.1fs  %d 字 ≈ %d 秒  分镜 %d 个",
		time.Since(start).Seconds(), short.BodyLen, short.SpeechSeconds, len(short.Segments))
	t.Logf("标题: %s", short.Title)
	for _, s := range short.Segments {
		t.Logf("  [%s] 画面: %s", s.At, s.Shot)
		t.Logf("        台词: %s", s.Line)
	}
	if len(short.Segments) < 3 {
		t.Errorf("口播稿分镜只有 %d 个，至少要 4 个", len(short.Segments))
	}
	if short.SpeechSeconds < 40 || short.SpeechSeconds > 100 {
		t.Errorf("口播时长 %d 秒，不在 60-90 秒附近", short.SpeechSeconds)
	}
}
