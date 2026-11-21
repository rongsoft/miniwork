package publish

import "testing"

// 视口尺寸的上下界。
//
// 这两个界不是审美：太小平台会切成手机版、表单一列，用户照着屏幕找不到
// 发布按钮；太大每帧体积翻几倍，滚动时界面就跟不上了。所以它们是硬约束，
// 界面报什么都要夹回这个区间。
func TestClampViewport(t *testing.T) {
	cases := []struct {
		inW, inH, wantW, wantH int
		why                    string
	}{
		{1000, 700, 1000, 700, "正常尺寸原样返回"},
		{100, 100, liveMinW, liveMinH, "太小的拉到下限"},
		{5000, 5000, liveMaxW, liveMaxH, "太大的压到上限"},
		{0, 0, liveMinW, liveMinH, "零值当下限（第一次打开还没量到尺寸）"},
		{-50, 800, liveMinW, 800, "负宽度当没量到"},
		{liveMaxW + 1, 700, liveMaxW, 700, "刚好超一像素也压回去"},
	}
	for _, c := range cases {
		gotW, gotH := clampViewport(c.inW, c.inH)
		if gotW != c.wantW || gotH != c.wantH {
			t.Errorf("%s: clampViewport(%d,%d) = %d,%d，想要 %d,%d",
				c.why, c.inW, c.inH, gotW, gotH, c.wantW, c.wantH)
		}
	}
}

// 不认识的操做要报错，不能静默吞掉。
//
// 静默吞掉的后果是「点了没反应」——用户会一直点，而日志里什么都没有。
func TestDoInputRejectsUnknownKind(t *testing.T) {
	err := doInput(t.Context(), LiveInput{Kind: "telepathy"})
	if err == nil {
		t.Fatal("未知操作应当报错")
	}
}

// 空文本不该发出去。
//
// 输入框里敲退格会把 value 清空，这时 input 事件照样触发一次；
// 把空字符串投过去，页面会当成「插入了一个空串」——大部分编辑器会
// 因此把光标位置挪一下，表现成「打字时光标乱跳」。
func TestDoInputIgnoresEmptyText(t *testing.T) {
	if err := doInput(t.Context(), LiveInput{Kind: "text", Text: ""}); err != nil {
		t.Fatalf("空文本应当直接返回，不该报错: %v", err)
	}
}

// 方向为 0 的滚轮事件没有意义，也别投。
//
// 触控板的惯性滚动会连着发一堆 delta 为 0 的事件，全投过去只是白跑 CDP。
func TestDoInputIgnoresZeroWheel(t *testing.T) {
	if err := doInput(t.Context(), LiveInput{Kind: "wheel"}); err != nil {
		t.Fatalf("零位移滚轮应当直接返回: %v", err)
	}
}
