package ai

import (
	"strings"
	"testing"
)

// 拆解这一版的产品决定是「六行短字段」。解析层要负责把模型的惯性挡在外面：
// 它一定会在字段里塞换行、塞项目符号、塞一整段分析。
// 这些用例锁的就是「不管模型怎么啰嗦，落到界面上都是一行」。
func TestParseTeardownCard(t *testing.T) {
	raw := "好的，我来分析一下：\n```json\n" + `{
		"title": "数字+身份，把结果前置",
		"cover": "未提供封面，看不出",
		"structure": "身份立信→反常识结论→具体场景",
		"engage": "结尾问「你遇到过吗」",
		"tone": "冷静复盘，带自嘲",
		"topics": ["职场", "求职", "转型", "多余的第4个"],
		"learn": "标题先给数字再给身份，正文每段配一个具体场景。",
		"category": "职场/成长"
	}` + "\n```\n希望有帮助。"

	td, err := ParseTeardown(raw)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if td.Title != "数字+身份，把结果前置" {
		t.Errorf("标题亮点 = %q", td.Title)
	}
	if td.Structure != "身份立信→反常识结论→具体场景" {
		t.Errorf("内容结构 = %q", td.Structure)
	}
	if td.Learn == "" || td.Category != "职场/成长" {
		t.Errorf("可借鉴点/分类丢了: %+v", td)
	}
	// 卡片上最多三个标签：多了会把「适合话题」那一行挤成两行。
	if len(td.Topics) != 3 {
		t.Errorf("话题应截到 3 个，实际 %d 个: %v", len(td.Topics), td.Topics)
	}
}

func TestParseTeardownNeedsSubstance(t *testing.T) {
	// 模型只回一句评价，不算拆出来。用户要的是能照做的东西。
	if _, err := ParseTeardown(`{"title":"这条很好","tone":"有感染力"}`); err == nil {
		t.Fatal("只有两个空字段时应该报错")
	}
	// 结构或可借鉴点至少有一个，否则卡片上没有一条能落地的信息。
	if _, err := ParseTeardown(`{"title":"a","cover":"b","engage":"c","tone":"d"}`); err == nil {
		t.Fatal("既没结构也没可借鉴点，应该报错")
	}
	if _, err := ParseTeardown("模型今天不想干活"); err == nil {
		t.Fatal("没有 JSON 时应该报错")
	}
}

func TestCardLine(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"去换行", "数字+身份\n后面的解释都丢掉", "数字+身份"},
		{"跳过标签行", "标题亮点：\n数字+身份，结果前置", "数字+身份，结果前置"},
		{"去项目符号", "- 先给结论再补细节", "先给结论再补细节"},
		{"去序号", "1. 疑问开场→3个方法", "疑问开场→3个方法"},
		{"折叠空白", "冷   静  复盘", "冷 静 复盘"},
		{"空串", "   \n  ", ""},
		{"已经是短句就原样", "结尾提问，引评论", "结尾提问，引评论"},
	}
	for _, c := range cases {
		if got := cardLine(c.in, 40); got != c.want {
			t.Errorf("%s: cardLine(%q) = %q，期望 %q", c.name, c.in, got, c.want)
		}
	}

	// 超长必须截断，否则卡片又变回一段话。
	long := strings.Repeat("啰", 100)
	if got := []rune(cardLine(long, 20)); len(got) != 21 || got[20] != '…' {
		t.Errorf("超长应截成 20 字加省略号，实际 %d 字: %s", len(got), string(got))
	}
}

// 提示词里那几条硬约束是产品要求，不是措辞偏好。
// 有人「顺手润色」把限字数删掉，这条测试会拦住。
func TestTeardownPromptConstraints(t *testing.T) {
	system, user := BuildTeardownPrompt("标题", "", Brief{Niche: "职场"})
	for _, must := range []string{"不超过 20 字", "structure", "learn", "可借鉴点"} {
		if !strings.Contains(system, must) {
			t.Errorf("提示词里缺了 %q", must)
		}
	}
	// 只给标题时必须明确禁止编造：不然模型会凭空写一段封面设计。
	if !strings.Contains(user, "不许猜") {
		t.Error("没给正文时，提示词必须禁止猜测")
	}
}
