package source

import (
	"context"
	"strings"
	"testing"
)

// 关键词搜索要能命中语料里的 [标记]，而不只是标题字面。
//
// 「被优化那天，我才明白公司从来不是家」标题里没有「裁员」，
// 标记里有 [优化]——这类同义表达如果搜不到，关键词挖掘就废了一半。
func TestSearchByKeyword(t *testing.T) {
	m := newMock(Spec{MockSeed: 1})
	got, err := m.Fetch(context.Background(), Query{Keyword: "裁员"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("搜「裁员」应该有结果，语料里有多条")
	}
	for _, h := range got {
		if !strings.Contains(h.Title, "裁员") {
			t.Fatalf("结果里混进了不相关的内容：%s", h.Title)
		}
	}
}

// 关键词搜索必须跨赛道。
//
// 用户搜「副业」时，职场赛道里「35 岁被裁员后，我用 3 个月把副业做成了主业」
// 同样是他要的。只搜当前赛道会漏掉一半。
func TestSearchAcrossNiches(t *testing.T) {
	m := newMock(Spec{MockSeed: 1})
	got, err := m.Fetch(context.Background(), Query{Niche: "职场", Keyword: "副业"})
	if err != nil {
		t.Fatal(err)
	}
	niches := map[string]bool{}
	for _, h := range got {
		for _, tag := range h.Tags {
			niches[tag] = true
		}
	}
	if len(niches) < 2 {
		t.Fatalf("搜「副业」应跨到多个赛道，实际只有 %v", niches)
	}
}

// 搜不到就返回空，不造数据。
// 编一条「宠物经济爆发」比空结果更糟：用户会以为数据源真能搜，
// 然后拿一份假素材去写稿。
func TestSearchNoResult(t *testing.T) {
	m := newMock(Spec{MockSeed: 1})
	got, err := m.Fetch(context.Background(), Query{Keyword: "量子纠缠"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("不该编造结果，得到 %d 条：%s", len(got), got[0].Title)
	}
}

// 同一个关键词搜两次，结果顺序必须一致。
//
// 用随机数生成热度的话，第二次搜同一条会出现不同热度，
// 用户会以为数据在乱跳，进而怀疑所有数字。
func TestSearchStable(t *testing.T) {
	m := newMock(Spec{MockSeed: 1})
	a, err := m.Fetch(context.Background(), Query{Keyword: "裁员"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := m.Fetch(context.Background(), Query{Keyword: "裁员"})
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != len(b) {
		t.Fatalf("两次搜索条数不同：%d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i].ID != b[i].ID {
			t.Fatalf("第 %d 条顺序不一致：%s vs %s", i, a[i].Title, b[i].Title)
		}
		if a[i].Heat != b[i].Heat {
			t.Fatalf("同一条热度却变了：%s %d → %d", a[i].Title, a[i].Heat, b[i].Heat)
		}
	}
}

// 有关键词时可以不填赛道——用户想找的是「所有讲这个的」。
func TestSearchWithoutNiche(t *testing.T) {
	m := newMock(Spec{MockSeed: 1})
	if _, err := m.Fetch(context.Background(), Query{Keyword: "汇报"}); err != nil {
		t.Fatalf("只给关键词应该能搜，得到错误：%v", err)
	}
	// 两个都空就没法查了，必须报错而不是返回全量。
	if _, err := m.Fetch(context.Background(), Query{}); err == nil {
		t.Fatal("赛道和关键词都为空时应该报错")
	}
}

func TestMatchKeyword(t *testing.T) {
	cases := []struct {
		mark, kw string
		want     bool
	}{
		{"裁员", "裁员", true},
		{"裁员", "裁员赔偿", true}, // 用户输入更长
		{"裁员赔偿", "裁员", true}, // 标记更长
		{"裁员", "优化", false},
	}
	for _, c := range cases {
		if got := matchKeyword(c.mark, c.kw); got != c.want {
			t.Fatalf("matchKeyword(%q, %q) = %v，期望 %v", c.mark, c.kw, got, c.want)
		}
	}
}

func TestMarks(t *testing.T) {
	got := marks("35岁[裁员]后，我用3个月把[副业]做成了主业")
	if len(got) != 2 || got[0] != "裁员" || got[1] != "副业" {
		t.Fatalf("标记提取错误：%v", got)
	}
	if len(marks("没有标记的标题")) != 0 {
		t.Fatal("没有标记时应返回空")
	}
}
