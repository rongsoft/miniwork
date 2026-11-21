package draft

import (
	"strings"
	"testing"
)

func TestCheckReportsTruncation(t *testing.T) {
	sp, ok := Get("xiaohongshu")
	if !ok {
		t.Fatal("小红书规格丢了")
	}
	// 21 个字，比上限多一个。
	title := strings.Repeat("字", 21)
	issues := Check(sp, Long, title, "正文", []string{"职场"})

	var got string
	for _, is := range issues {
		if is.Field == "title" && strings.Contains(is.Text, "看不见") {
			got = is.Text
		}
	}
	// 报「超了 1 个字」是没用的，用户要的是「从第几个字开始看不见」。
	if got == "" {
		t.Fatalf("标题超限应提示会被截断，实际 %+v", issues)
	}
	if !strings.Contains(got, "21") || !strings.Contains(got, "20") {
		t.Errorf("提示要带上实际字数与上限，实际 %q", got)
	}
}

func TestCheckPassesWithinLimit(t *testing.T) {
	sp, _ := Get("wechat")
	issues := Check(sp, Long, "一篇正常的标题", strings.Repeat("字", 500), []string{"a", "b"})
	if len(issues) != 0 {
		t.Fatalf("全在限内不该有提示，实际 %+v", issues)
	}
}

func TestCheckCountsRunesNotBytes(t *testing.T) {
	sp, _ := Get("xiaohongshu")
	// 20 个汉字 = 60 字节。按字节算会误报超限。
	title := strings.Repeat("字", 20)
	for _, is := range Check(sp, Long, title, "正文", nil) {
		if is.Field == "title" {
			t.Fatalf("20 个汉字正好等于上限，不该报: %s", is.Text)
		}
	}
}

func TestBuildAddsHashForChinesePlatforms(t *testing.T) {
	sp, _ := Get("xiaohongshu")
	p := Build(sp, "标题", "正文", []string{"#职场", " 副业 ", "职场"})
	if len(p.Tags) != 2 {
		t.Fatalf("标签应去重并剥掉井号，实际 %v", p.Tags)
	}
	if !strings.Contains(p.Text, "#职场 #副业") {
		t.Errorf("小红书的话题要带 #，实际 %q", p.Text)
	}
	if strings.Contains(p.Text, "##") {
		t.Error("不该出现双井号")
	}
}

func TestBuildWechatKeepsPlainWords(t *testing.T) {
	sp, _ := Get("wechat")
	p := Build(sp, "标题", "正文", []string{"职场", "副业"})
	// 公众号没有话题体系，井号会原样显示在正文里。
	if strings.Contains(p.Text, "#") {
		t.Errorf("公众号不该出现 #，实际 %q", p.Text)
	}
	if !strings.Contains(p.Text, "职场、副业") {
		t.Errorf("公众号的关键词用顿号分隔，实际 %q", p.Text)
	}
}

func TestBuildWithoutTags(t *testing.T) {
	sp, _ := Get("douyin")
	p := Build(sp, "标题", " 正文 ", nil)
	if p.Text != "正文" {
		t.Fatalf("没有标签时正文不该多出空行，实际 %q", p.Text)
	}
}

func TestGetUnknownPlatform(t *testing.T) {
	if _, ok := Get("weibo"); ok {
		t.Fatal("未知平台不该返回规格")
	}
}

func TestAllCoversThreePlatforms(t *testing.T) {
	all := All()
	if len(all) < 3 {
		t.Fatalf("至少要有小红书/公众号/抖音，实际 %d 个", len(all))
	}
	// 返回副本：调用方改返回的切片不能污染内部表。
	all[0].Name = "改坏了"
	if again := All(); again[0].Name == "改坏了" {
		t.Fatal("All 必须返回副本")
	}
}

func TestCheckSpeechUsesDurationNotBoxLimit(t *testing.T) {
	sp, _ := Get("douyin")

	// 500 字：远低于抖音 1000 字的文案框上限，但念出来接近两分钟。
	issues := Check(sp, Short, "标题", strings.Repeat("字", 500), nil)
	var found bool
	for _, is := range issues {
		if is.Field == "body" && strings.Contains(is.Text, "90 秒") {
			found = true
		}
	}
	if !found {
		t.Fatalf("口播稿要按时长判，不能按文案框上限判，实际 %+v", issues)
	}

	// 200 字撑不到 60 秒。
	issues = Check(sp, Short, "标题", strings.Repeat("字", 200), nil)
	found = false
	for _, is := range issues {
		if is.Field == "body" && strings.Contains(is.Text, "60 秒") {
			found = true
		}
	}
	if !found {
		t.Fatalf("太短也要提示，实际 %+v", issues)
	}
}

func TestSpeechSeconds(t *testing.T) {
	if got := SpeechSeconds(""); got != 0 {
		t.Errorf("空台词应为 0 秒，实际 %d", got)
	}
	// 270 个字约 60 秒。
	if got := SpeechSeconds(strings.Repeat("字", 270)); got < 58 || got > 62 {
		t.Errorf("270 字应约 60 秒，实际 %d", got)
	}
}
