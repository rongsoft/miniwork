package score

import (
	"strings"
	"testing"
	"time"

	"rongsoft/miniwork/app/internal/source"
)

// 这条是整个偏好加分层最重要的一条测试：
// 跑题内容就算把学到的词全命中，也一分都不能加。
//
// 理由不是「小心点」，而是结构性的：偏好是从历史里学的，天生会让人越看越窄。
// 如果跑题内容也能蹭到加分，列表会慢慢变成「你的历史回声」而不是「你的赛道」。
func TestLearntNeverBoostsOffTopic(t *testing.T) {
	now := time.Now()
	h := source.Hotspot{
		Title:       "新款手机发布，网友晒出夜景样张对比",
		Heat:        900000,
		PublishedAt: now.Add(-1 * time.Hour),
	}
	base := Score(h, "职场", now)
	if base.NicheHit != 0 {
		t.Fatalf("这条不该命中职场词，得到 %d", base.NicheHit)
	}
	got := ApplyLearnt(base, h.Title, []string{"夜景", "样张"})
	if got.Score != base.Score {
		t.Fatalf("跑题内容不该拿偏好分：%d → %d", base.Score, got.Score)
	}
	if strings.Contains(got.Reason, "你常收") {
		t.Fatalf("跑题内容不该出现偏好理由，得到 %q", got.Reason)
	}
}

func TestLearntAddsWithinCap(t *testing.T) {
	now := time.Now()
	h := source.Hotspot{
		Title:       "跟领导汇报工作的三个坑",
		Heat:        200000,
		PublishedAt: now.Add(-3 * time.Hour),
	}
	base := Score(h, "职场", now)
	if base.NicheHit == 0 {
		t.Fatal("测试前提：这条应命中职场词")
	}

	one := ApplyLearnt(base, h.Title, []string{"汇报"})
	if one.Score != base.Score+learnedPerWord {
		t.Fatalf("命中一个词应 +%d，%d → %d", learnedPerWord, base.Score, one.Score)
	}
	if !strings.Contains(one.Reason, "汇报") {
		t.Fatalf("理由里要写明是哪个词，得到 %q", one.Reason)
	}

	// 五个词全命中，加分必须停在封顶。
	many := ApplyLearnt(base, h.Title, []string{"汇报", "领导", "工作", "三个", "的坑"})
	if many.Score != base.Score+learnedCap {
		t.Fatalf("加分应封顶 %d，得到 %d（基线 %d）", learnedCap, many.Score, base.Score)
	}
}

// 封顶之后总分不能超过 100。界面上分数旁边写着「满分 100」，
// 出现 104 分是明显的数据错误，而且会让排序和进度条都算错。
func TestLearntNeverExceeds100(t *testing.T) {
	now := time.Now()
	h := source.Hotspot{
		Title:       "35岁被裁员后跳槽涨薪，面试官问我的三个问题",
		Heat:        50000000,
		PublishedAt: now.Add(-30 * time.Minute),
	}
	base := Score(h, "职场", now)
	got := ApplyLearnt(base, h.Title, []string{"裁员", "跳槽", "面试", "涨薪", "官问"})
	if got.Score > 100 {
		t.Fatalf("总分不该超过 100，得到 %d（基线 %d）", got.Score, base.Score)
	}
}

// 被人设否决的内容不许靠偏好分救回来。
// 否决是用户明确说的「不碰」，偏好是他没说过、系统猜的——
// 猜的东西不能盖过明说的话。
func TestVetoBeatsLearnt(t *testing.T) {
	now := time.Now()
	h := source.Hotspot{
		Title:       "裁员赔偿怎么算，劳动仲裁全流程",
		Heat:        300000,
		PublishedAt: now.Add(-4 * time.Hour),
	}
	p := Prefs{Avoid: "不碰裁员的赔偿法律细节"}
	r := Evaluate(h, "职场", now, p, []string{"裁员", "赔偿", "仲裁"})
	if !r.Vetoed {
		t.Fatal("测试前提：这条应被否决")
	}
	if strings.Contains(r.Reason, "你常收") {
		t.Fatalf("被否决的内容不该出现偏好理由，得到 %q", r.Reason)
	}
	// 否决后总分必须低于准入线，否则这条还是会出现在当天的列表里。
	if r.Score >= 50 {
		t.Fatalf("被否决的选题应低于准入线 50，得到 %d（理由 %q）", r.Score, r.Reason)
	}
}

// Evaluate 的顺序是被结论验证过的：规则 → 否决 → 偏好。
// 顺序写反不会报错，只会让「我填的禁忌没生效」在某个场景下复现，
// 所以这里直接钉住行为。
func TestEvaluateOrder(t *testing.T) {
	now := time.Now()
	h := source.Hotspot{
		Title:       "领导让我汇报，我却把裁员的事说了",
		Heat:        150000,
		PublishedAt: now.Add(-2 * time.Hour),
	}
	p := Prefs{Avoid: "法律"}

	// 没有偏好词时，Evaluate 应等价于 ApplyPrefs(Score(...))。
	want := ApplyPrefs(Score(h, "职场", now), h.Title, p)
	got := Evaluate(h, "职场", now, p, nil)
	if got.Score != want.Score {
		t.Fatalf("无偏好词时应与人设链一致：%d vs %d", got.Score, want.Score)
	}
	if got.Vetoed != want.Vetoed {
		t.Fatalf("Vetoed 标记应透传：%v vs %v", got.Vetoed, want.Vetoed)
	}
}

// 空词表要能安全通过：用户还没收过东西时走的就是这条路。
func TestApplyLearntEmptyWords(t *testing.T) {
	now := time.Now()
	h := source.Hotspot{Title: "领导让我汇报", Heat: 1000, PublishedAt: now}
	base := Score(h, "职场", now)
	if got := ApplyLearnt(base, h.Title, nil); got.Score != base.Score {
		t.Fatalf("空词表不该改变分数：%d → %d", base.Score, got.Score)
	}
}

func TestNicheWordsExported(t *testing.T) {
	if len(NicheWords("职场")) == 0 {
		t.Fatal("NicheWords 要能取到赛道词，记忆层靠它做排除")
	}
	if len(NicheWords("不存在的赛道")) != 0 {
		t.Fatal("未知赛道应返回空，不能 panic")
	}
}
