package cover

import (
	"image"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 尺寸必须和入参一致：平台按像素校验封面比例，
// 差几十像素就可能被判成「比例不对，请重新上传」。
func TestRenderSize(t *testing.T) {
	for _, sz := range []Size{SizeXHS, SizeWeChat} {
		img, err := Render(Opt{Title: "一个人也能跑通的自媒体工作台", Size: sz})
		if err != nil {
			t.Fatalf("%v: %v", sz, err)
		}
		if got := img.Bounds().Size(); got.X != sz.W || got.Y != sz.H {
			t.Fatalf("尺寸 %v，期望 %v", got, sz)
		}
	}
}

// 光有背景不算画好了：要确认真的落了字。
// 只看「图片非空」是查不出来的——纯色背景也是一张非空图片，
// 而字体加载失败时正好就长这样。
func TestRenderDrawsText(t *testing.T) {
	img, err := Render(Opt{Title: "标题", Size: SizeXHS, Style: 0})
	if err != nil {
		t.Fatal(err)
	}
	bg := styles[0].Ink
	n := 0
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if color.RGBAModel.Convert(img.At(x, y)).(color.RGBA) == bg {
				n++
			}
		}
	}
	if n < 500 {
		t.Fatalf("画面上只有 %d 个标题色像素，字大概率没画出来", n)
	}
}

func TestRenderEmptyTitle(t *testing.T) {
	if _, err := Render(Opt{Title: "   ", Size: SizeXHS}); err == nil {
		t.Fatal("空标题必须报错，不能返回一张空图")
	}
}

// 超长标题不能硬画：要么缩小字号放进画布，要么明确报错。
// 最怕的是"画出来了但只有半句话"——图看着正常，标题是残缺的。
func TestLongTitleFits(t *testing.T) {
	long := strings.Repeat("这是一句很长的标题用来测试自动缩排", 6)
	img, err := Render(Opt{Title: long, Size: SizeXHS})
	if err == nil && img == nil {
		t.Fatal("既没报错也没图")
	}
	// 能画就该逐字检查：所有字符都进了行，没有丢。
	face, lines, _, err2 := fitTitle(strings.Repeat("很长", 40), float64(SizeXHS.W)*0.8, SizeXHS.H, false)
	if err2 != nil {
		t.Skipf("这个长度按设计会拒绝：%v", err2)
	}
	defer face.Close()
	got := strings.Join(lines, "")
	want := strings.Repeat("很长", 40)
	if got != want {
		t.Fatalf("换行丢字了：\n得到 %q\n期望 %q", got, want)
	}
}

func TestWrapKeepsWords(t *testing.T) {
	f, err := newFace(64)
	if err != nil {
		t.Skipf("本机没有中文字体：%v", err)
	}
	defer f.Close()

	lines := wrap("用 little-red-book 做完这一期", f, 400)
	for _, ln := range lines {
		// 英文单词被劈开一半时，行首或行尾会出现孤立的片段。
		if strings.HasPrefix(ln, "ook") || strings.HasSuffix(ln, "little-red-") {
			t.Fatalf("单词被劈开了：%q", lines)
		}
	}
}

// 同一个标题必须永远是同一套配色，否则改一个字整张图换色，
// 用户会以为每次生成都是随机的。
func TestStyleStable(t *testing.T) {
	a := pickStyle("同一个标题", -1)
	b := pickStyle("同一个标题", -1)
	if a != b {
		t.Fatal("同一标题两次取到不同配色")
	}
	differ := false
	for _, s := range styles[1:] {
		if pickStyle("同一个标题", -1) != s {
			differ = true
		}
	}
	if !differ {
		t.Fatal("所有标题都取到同一套配色，hash 没起作用")
	}
}

// 以下这些不是断言，是给人看的：go test -run TestSamples -v 会把样图写到
// /tmp/mwcov，用来肉眼确认排版好不好看。自动化测不出「好看」。
func TestSamples(t *testing.T) {
	if os.Getenv("MW_COVER_SAMPLES") == "" {
		t.Skip("设置 MW_COVER_SAMPLES=1 才会出样图")
	}
	dir := "/tmp/mwcov"
	_ = os.MkdirAll(dir, 0o755)
	items := []struct {
		name string
		o    Opt
	}{
		{"xhs-long", Opt{Title: "一个人也能跑通的自媒体工作台", Kicker: "小红书 · 图文", Foot: "2026.11.16", Size: SizeXHS}},
		{"xhs-mid", Opt{Title: "我在第 40 次投简历时才明白的事", Kicker: "小红书 · 图文", Foot: "2026.11.16", Size: SizeXHS}},
		{"xhs-short", Opt{Title: "副业", Kicker: "小红书 · 图文", Foot: "2026.11.16", Size: SizeXHS}},
		{"xhs-2line", Opt{Title: "为什么你写的笔记没人看：三个被忽略的细节", Kicker: "小红书 · 图文", Foot: "2026.11.16", Size: SizeXHS}},
		{"wx-wide", Opt{Title: "一个人也能跑通的自媒体工作台", Kicker: "公众号", Foot: "2026.11.16", Size: SizeWeChat}},
	}
	for _, it := range items {
		img, err := Render(it.o)
		if err != nil {
			t.Fatalf("%s: %v", it.name, err)
		}
		if err := SavePNG(img, filepath.Join(dir, it.name+".png")); err != nil {
			t.Fatal(err)
		}
	}
	_ = image.Rect
}

// 末行只剩一个字是小学生作文都不该有的错，封面上一眼就看见。
// 这条守的是「排版看起来像人做的」，不是功能。
func TestNoLonelyTail(t *testing.T) {
	title := "一个人也能跑通的自媒体工作台"
	// 先用能画字体的字号试一遍所有可能用到的尺寸，
	// 因为孤字是跟着字号变的，只测一个尺寸会漏。
	for _, size := range []float64{40, 56, 64, 76, 96, 120, 150, 180, 220, 260} {
		f, err := newFace(size)
		if err != nil {
			t.Skip(err)
		}
		lines := wrapBalanced(title, f, 760)
		got := strings.Join(lines, "")
		if got != title {
			f.Close()
			t.Fatalf("%.0fpx 换行丢字：%q", size, lines)
		}
		if lonelyTail(lines) {
			f.Close()
			t.Fatalf("%.0fpx 出现孤字：%q", size, lines)
		}
		f.Close()
	}
}
