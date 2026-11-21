package score

import (
	"strings"
	"testing"
	"time"

	"rongsoft/miniwork/app/internal/source"
)

func TestRelevantContentScoresHigh(t *testing.T) {
	now := time.Now()
	h := source.Hotspot{
		Title:       "35岁被裁员后，我用3个月把副业做成了主业",
		Heat:        500000,
		PublishedAt: now.Add(-2 * time.Hour),
	}
	r := Score(h, "职场", now)
	if r.Score < 60 {
		t.Fatalf("高相关 + 高热度 + 新鲜的标题应 >= 60，实际 %d（因子 %+v）", r.Score, r.Factors)
	}
	if !strings.Contains(r.Reason, "裁员") {
		t.Errorf("理由应说明命中的赛道词，实际 %q", r.Reason)
	}
}

// 这条是产品的核心判断：跑题的爆款不能上榜。
func TestOffTopicIsCappedEvenIfViral(t *testing.T) {
	now := time.Now()
	h := source.Hotspot{
		Title:       "新款手机上手一周，说说真实感受",
		Heat:        980000,
		PublishedAt: now.Add(-10 * time.Minute),
	}
	r := Score(h, "职场", now)
	if r.Score > 42 {
		t.Fatalf("赛道无关内容应封顶 42，实际 %d", r.Score)
	}
	if !strings.Contains(r.Reason, "弱") {
		t.Errorf("理由应提示赛道关联弱，实际 %q", r.Reason)
	}
}

func TestFreshBeatsStaleWhenOtherFactorsEqual(t *testing.T) {
	now := time.Now()
	fresh := source.Hotspot{Title: "被裁员那天我才明白", Heat: 100000, PublishedAt: now.Add(-1 * time.Hour)}
	stale := source.Hotspot{Title: "被裁员那天我才明白", Heat: 100000, PublishedAt: now.Add(-60 * time.Hour)}

	rf := Score(fresh, "职场", now)
	rs := Score(stale, "职场", now)
	if rf.Score <= rs.Score {
		t.Fatalf("新鲜内容应高于陈旧内容：fresh=%d stale=%d", rf.Score, rs.Score)
	}
}

func TestScoreIsStableForSameInput(t *testing.T) {
	now := time.Now()
	h := source.Hotspot{Title: "为什么跳槽涨薪反而比内部晋升快？说点真话", Heat: 180000, PublishedAt: now.Add(-3 * time.Hour)}
	a := Score(h, "职场", now)
	b := Score(h, "职场", now)
	if a.Score != b.Score || a.Reason != b.Reason {
		t.Fatalf("同样输入应得到同样结果：%d/%q vs %d/%q", a.Score, a.Reason, b.Score, b.Reason)
	}
}

// 人设里的「不碰」必须在没有模型的时候也能生效，
// 否则用户填完那三问当场看不到任何变化，第二次就不会再填。
func TestApplyPrefsVetoesAvoidedTopic(t *testing.T) {
	base := Result{Score: 78, Reason: "命中「裁员」"}
	p := Prefs{Avoid: "不碰裁员赔偿的法律细节"}

	got := ApplyPrefs(base, "被裁员后我去申请劳动仲裁，赔了 2 个月", p)
	if got.Score != 48 {
		t.Fatalf("命中禁忌应扣 30 分，得到 %d", got.Score)
	}
	if !strings.Contains(got.Reason, "法律") {
		t.Fatalf("理由要说明是哪条禁忌，得到 %q", got.Reason)
	}
}

func TestApplyPrefsIgnoresUnrelatedTitle(t *testing.T) {
	base := Result{Score: 70, Reason: "命中「跳槽」"}
	got := ApplyPrefs(base, "跳槽前我算了笔账，发现亏的是自己", Prefs{Avoid: "不碰法律"})
	if got.Score != 70 {
		t.Fatalf("未命中禁忌不该扣分，得到 %d", got.Score)
	}
}

// 「暂时没有」是界面上那个示例按钮的文案，不能被当成禁忌词去扫。
func TestApplyPrefsSkipsEmptyAnswer(t *testing.T) {
	base := Result{Score: 70}
	if got := ApplyPrefs(base, "公司让我签赔偿协议", Prefs{Avoid: "暂时没有"}); got.Score != 70 {
		t.Fatalf("选了暂时没有就不该扣分，得到 %d", got.Score)
	}
}

// 扣完不能变负数：负数会让「按分数排序」出现一堆并列 0，复盘时分不清谁更差。
func TestApplyPrefsNeverNegative(t *testing.T) {
	got := ApplyPrefs(Result{Score: 12}, "劳动仲裁和赔偿", Prefs{Avoid: "法律"})
	if got.Score != 0 {
		t.Fatalf("扣分后应落到 0，得到 %d", got.Score)
	}
}
