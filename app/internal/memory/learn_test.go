package memory

import (
	"strings"
	"testing"
)

func hasWord(l Learning, text string) bool {
	for _, w := range l.Words {
		if w.Text == text {
			return true
		}
	}
	return false
}

// 样本不够时必须老实说不够，不能硬凑一个结论出来。
// 这是整个包最重要的一条：学错了比没学更伤——用户会照着错的判断做选择。
func TestNotEnoughSamples(t *testing.T) {
	l := Learn([]string{"领导让我汇报", "同事抢功"}, nil, nil)
	if l.Enough {
		t.Fatalf("两条不该下结论，得到 %+v", l.Words)
	}
	if l.Note == "" {
		t.Fatal("样本不足也要给一句话，不能空着")
	}
	if !strings.Contains(l.Note, "2 条") {
		t.Fatalf("提示里要带上已收下的条数，得到 %q", l.Note)
	}
}

func TestLearnRepeatedWord(t *testing.T) {
	picked := []string{
		"领导让我汇报，我这样说的",
		"跟领导汇报工作的三个坑",
		"汇报时别说这三句话",
		"同事抢了我的功劳",
	}
	l := Learn(picked, nil, nil)
	if !hasWord(l, "汇报") {
		t.Fatalf("「汇报」出现在 3 条里，应该学到，得到 %+v", l.Words)
	}
	for _, w := range l.Words {
		if w.Picked < minSupport {
			t.Fatalf("学到的词至少要有 %d 条证据，%q 只有 %d", minSupport, w.Text, w.Picked)
		}
	}
	if l.Picked != 4 {
		t.Fatalf("Picked 应记为 4，得到 %d", l.Picked)
	}
}

// 两边都沾的词没有区分度，不能学。
// 如果学了，「领导」这种到处都有的词会给一大票标题加分，等于把排序搅乱。
func TestSkipWordOnBothSides(t *testing.T) {
	picked := []string{
		"领导让我汇报", "跟领导汇报的三个坑", "汇报时别说这三句",
	}
	dropped := []string{
		"领导画饼怎么办", "领导说年轻人要吃苦", "领导最喜欢的三类人",
	}
	l := Learn(picked, dropped, nil)
	if hasWord(l, "领导") {
		t.Fatalf("「领导」收 3 丢 3 没有区分度，不该学，得到 %+v", l.Words)
	}
}

// 收 5 丢 4 也不学：比例不够悬殊。
// 只要求 picked > dropped 太松，很多词会刚好越过线。
func TestRatioMustBeClear(t *testing.T) {
	picked := []string{"副业赚钱", "副业怎么开始", "副业踩过的坑", "副业时间管理", "副业第一年"}
	dropped := []string{"副业亏了", "副业别做", "副业真相", "副业劝退"}
	l := Learn(picked, dropped, nil)
	if hasWord(l, "副业") {
		t.Fatalf("5:4 说明这个词两边都沾，不该学，得到 %+v", l.Words)
	}
}

// 已有「汇报」时，包含它的更长片段不重复计分。
func TestSubstringSuppression(t *testing.T) {
	picked := []string{
		"汇报工作的三个坑", "汇报工作别踩雷", "汇报工作这样说",
	}
	l := Learn(picked, nil, nil)
	if !hasWord(l, "汇报") {
		t.Fatalf("应学到「汇报」，得到 %+v", l.Words)
	}
	for _, w := range l.Words {
		if w.Text != "汇报" && strings.Contains(w.Text, "汇报") {
			t.Fatalf("「%s」被「汇报」覆盖，不该重复入选", w.Text)
		}
	}
}

// 赛道词由调用方排除：赛道匹配已经占了 40 分，再按偏好加一次就是重复计分。
func TestExcludeNicheWords(t *testing.T) {
	picked := []string{"裁员的三个信号", "裁员前我做了什么", "裁员后被问最多的问题"}
	l := Learn(picked, nil, []string{"裁员"})
	if hasWord(l, "裁员") {
		t.Fatalf("排除词不该出现，得到 %+v", l.Words)
	}
}

// 虚词能通过频次检验，但学它毫无意义。
func TestSkipStopWords(t *testing.T) {
	picked := []string{
		"这个时候我该怎么办", "那个时候没人告诉我", "什么时候该走",
	}
	l := Learn(picked, nil, nil)
	for _, w := range l.Words {
		if w.Text == "时候" || w.Text == "这个" || w.Text == "那个" {
			t.Fatalf("虚词不该学，得到 %q", w.Text)
		}
	}
}

func TestMaxWords(t *testing.T) {
	// 构造 10 个各自出现 3 次的词，结果必须被截到 maxWords。
	// 断言「恰好等于」而不是「不超过」：后者在学到 0 个词时也会通过，
	// 那这个测试就变成永远绿的摆设了。
	var picked []string
	for _, w := range []string{"甲组", "乙组", "丙组", "丁组", "戊组", "己组", "庚组", "辛组", "壬组", "癸组"} {
		for _, n := range []string{"第一次", "第二次", "第三次"} {
			picked = append(picked, w+n)
		}
	}
	l := Learn(picked, nil, nil)
	if len(l.Words) != maxWords {
		t.Fatalf("10 个候选应截到 %d 个，得到 %d: %+v", maxWords, len(l.Words), l.Words)
	}
}

// 收了很多条但完全没共同点，要说「没找到」而不是硬凑。
func TestNoPatternWhenScattered(t *testing.T) {
	picked := []string{"今天下雨了", "周末去爬山", "买了新键盘", "冰箱该清了"}
	l := Learn(picked, nil, nil)
	if l.Enough {
		t.Fatalf("没有共同点不该说学到了，得到 %+v", l.Words)
	}
	if !strings.Contains(l.Note, "分散") {
		t.Fatalf("要解释为什么没学到，得到 %q", l.Note)
	}
}

// ngrams 不能跨越标点切出假词。
func TestNgramsNoCrossPunctuation(t *testing.T) {
	for _, g := range ngrams("裁员，后我把") {
		if strings.Contains(g, "员后") {
			t.Fatalf("跨标点切出了假词 %q", g)
		}
	}
}

// 数字、英文、标点不该进候选。
func TestNgramsOnlyHan(t *testing.T) {
	for _, g := range ngrams("3 个 HR 的建议 ABC") {
		for _, r := range g {
			if !isHan(r) {
				t.Fatalf("候选 %q 里混进了非汉字", g)
			}
		}
	}
}
