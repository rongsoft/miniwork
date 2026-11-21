package tray

import (
	"bytes"
	"image"
	"image/png"
	"testing"
)

// 图标必须是能解码的 22×22 PNG。
//
// 这个断言看着废话，但它挡住的是最坏的一种失败：图片编码失败时
// SetIcon 拿到空字节，程序照常启动，只是菜单栏里什么都没有，
// 用户只会觉得「这软件坏了」而说不出哪里坏。
func TestIconDecodes(t *testing.T) {
	for name, tc := range map[string]struct {
		get  func() ([]byte, error)
		size int
	}{
		"light":    {Light, Size},
		"dark":     {Dark, Size},
		"template": {Template, Size},
		"deskOn":   {DeskLight, DeskSize},
		"deskOff":  {DeskDark, DeskSize},
	} {
		raw, err := tc.get()
		if err != nil {
			t.Fatalf("%s: 生成失败: %v", name, err)
		}
		if len(raw) == 0 {
			t.Fatalf("%s: 生成了空字节，托盘会什么都不显示", name)
		}
		img, err := png.Decode(bytes.NewReader(raw))
		if err != nil {
			t.Fatalf("%s: 不是合法 PNG: %v", name, err)
		}
		b := img.Bounds()
		if b.Dx() != tc.size || b.Dy() != tc.size {
			t.Fatalf("%s: 尺寸应为 %d×%d，实际 %d×%d", name, tc.size, tc.size, b.Dx(), b.Dy())
		}
	}
}

// 四角必须是透明的。
//
// 不透明的话，菜单栏里会出现一个带方块底色的图标——在 macOS 上
// 一眼就是「这图标没做圆角/没抠干净」，非常显眼。
func TestCornersTransparent(t *testing.T) {
	img := decode(t, Template)
	b := img.Bounds()
	corners := [][2]int{
		{b.Min.X, b.Min.Y},
		{b.Max.X - 1, b.Min.Y},
		{b.Min.X, b.Max.Y - 1},
		{b.Max.X - 1, b.Max.Y - 1},
	}
	for _, c := range corners {
		_, _, _, a := img.At(c[0], c[1]).RGBA()
		if a != 0 {
			t.Fatalf("角 (%d,%d) 不透明，alpha=%d", c[0], c[1], a)
		}
	}
}

// 图形得真的画出来了：不能是整张透明（画歪了/尺寸算错），
// 也不能是整张实心（圆角矩形判成矩形，图标会变成一块砖）。
func TestInkCoverageIsSane(t *testing.T) {
	img := decode(t, Template)
	b := img.Bounds()
	solid, total := 0, 0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			total++
			if _, _, _, a := img.At(x, y).RGBA(); a > 0x8000 {
				solid++
			}
		}
	}
	ratio := float64(solid) / float64(total)
	// 三行图形大约占三成到五成。给宽一点的区间：这是防「画歪了」，
	// 不是校样，调样式时不该因为挪了 1px 就红。
	if ratio < 0.15 || ratio > 0.6 {
		t.Fatalf("实心像素占比 %.2f 不合理：太低说明没画出来，太高说明糊成一整块", ratio)
	}
}

// 三条线的高度必须一致，横向必须能区分出「一行一行」。
//
// 具体查每行中线上的像素：三行中线处不透明，行与行之间要透气，
// 否则缩小到菜单栏里会糊成一根竖条。
func TestRowsAreSeparated(t *testing.T) {
	img := decode(t, Template)
	// 取第二个点所在的行（y=11）作为中线，检查它的上方和下方是空的。
	opaque := func(x, y int) bool {
		_, _, _, a := img.At(x, y).RGBA()
		return a > 0x8000
	}
	if !opaque(3, 11) {
		t.Fatal("第二行的点没画在 y=11，图形位置算错了")
	}
	// 行间距 6px，图形本身高度 2~3px，中间必然有空隙。
	if opaque(3, 8) {
		t.Fatal("y=8 不该有像素，三行糊在一起了")
	}
	if opaque(3, 14) {
		t.Fatal("y=14 不该有像素，三行糊在一起了")
	}
}

// dark 版必须是浅色线条：Windows 深色任务栏上用黑色线条等于看不见。
func TestDarkIsLightInk(t *testing.T) {
	img := decode(t, Dark)
	r, g, bl, a := img.At(3, 11).RGBA()
	if a == 0 {
		t.Fatal("dark 版在第二行的点上没有像素")
	}
	if r>>8 < 0xF0 || g>>8 < 0xF0 || bl>>8 < 0xF0 {
		t.Fatalf("dark 版线条应该是白色，实际 RGB=(%d,%d,%d)", r>>8, g>>8, bl>>8)
	}
}

// 桌面托盘的两份必须是**一深一浅**，配反了就是「浅色任务栏上白线」。
//
// 这条断言挡的是命名读反：DeskLight 返回深色墨（配浅底），
// DeskDark 返回浅色墨（配深底）。看名字猜正好猜反。
func TestDeskIconsAreOpposite(t *testing.T) {
	on := decode(t, DeskLight)
	off := decode(t, DeskDark)

	// 桌面版 48×48，中线在 5/11/17 × (48/22)，取第二行点所在像素。
	k := float64(DeskSize) / float64(Size)
	y := int(11 * k)
	x := int(3.4 * k)

	r, g, b, a := on.At(x, y).RGBA()
	if a == 0 {
		t.Fatal("DeskLight 在该位置没有像素")
	}
	if r>>8 > 0x0F || g>>8 > 0x0F || b>>8 > 0x0F {
		t.Fatalf("DeskLight 应该是深色墨（配浅色任务栏），实际 RGB=(%d,%d,%d)", r>>8, g>>8, b>>8)
	}

	r, g, b, a = off.At(x, y).RGBA()
	if a == 0 {
		t.Fatal("DeskDark 在该位置没有像素")
	}
	if r>>8 < 0xF0 || g>>8 < 0xF0 || b>>8 < 0xF0 {
		t.Fatalf("DeskDark 应该是浅色墨（配深色任务栏），实际 RGB=(%d,%d,%d)", r>>8, g>>8, b>>8)
	}
}

// 尺寸放大后图形比例不能跑偏：48 版的实心占比要和 22 版基本一致。
//
// 采样点是按比例缩的，只要缩错一个方向（比如 x 用 k、y 用 1），
// 图形就会被拉扁或撑满，占比立刻变得离谱。
func TestDeskScaleKeepsProportion(t *testing.T) {
	ratio := func(img image.Image) float64 {
		b := img.Bounds()
		solid, total := 0, 0
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				total++
				if _, _, _, a := img.At(x, y).RGBA(); a > 0x8000 {
					solid++
				}
			}
		}
		return float64(solid) / float64(total)
	}
	small := ratio(decode(t, Template))
	big := ratio(decode(t, DeskLight))
	if diff := small - big; diff > 0.06 || diff < -0.06 {
		t.Fatalf("放大后实心占比从 %.3f 变成 %.3f，图形比例跑偏了", small, big)
	}
}

func decode(t *testing.T, get func() ([]byte, error)) image.Image {
	t.Helper()
	raw, err := get()
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	return img
}
