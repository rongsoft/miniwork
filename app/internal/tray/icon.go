// Package tray 生成常驻菜单栏用的图标。
//
// 为什么要自己画：Wails 模板自带的是那个黑色 W，本来就是要换掉的东西，
// 拿它当托盘图标等于把待办里的丑图标又露一次。而引入一张 22×22 的图片
// 意味着仓库里多一个二进制文件、打包时多一份资源、改颜色要重新导出。
// 画在代码里就三个好处：零资源、深浅两套色一起出、改样式只改常数。
//
// 图形取「清单」：三行，每行一个点加一条线。这个产品的本质是清单，
// 菜单栏里认得出来，也不需要缩小后还能看清细节的复杂度。
package tray

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"sync"
)

// Size 是图标的边长。macOS 菜单栏高 22pt，PNG 没带 scale 标记时
// NSImage 按像素当点用，所以这里就是 22，正好铺满一行。
const Size = 22

// super 是超采样倍数。22×22 上直接画圆和圆头线会出锯齿，
// 先画 88×88 再平均降采样，边缘就平了——比手写距离场简单，效果够用。
const super = 4

// shape 是一个圆头矩形（r 取半高时就是胶囊形），三条横线和三个点都用它拼。
type shape struct {
	cx, cy float64 // 中心
	hw, hh float64 // 半宽、半高
	r      float64 // 圆角半径
}

// inside 判断点是否落在圆头矩形内。
//
// 做法是把矩形内缩 r 得到内核，内核之外按到内核的距离判圆角：
// 横向纵向都缩完还在里面的是实心区，只缩了一个方向的是直边区，
// 两个方向都不在的才需要算到角的距离。
func (s shape) inside(x, y float64) bool {
	dx := math.Abs(x-s.cx) - (s.hw - s.r)
	dy := math.Abs(y-s.cy) - (s.hh - s.r)
	if dx <= 0 && dy <= 0 {
		return true
	}
	if dx < 0 {
		dx = 0
	}
	if dy < 0 {
		dy = 0
	}
	return math.Hypot(dx, dy) <= s.r
}

// shapes 是要画的全部图形，坐标以 22×22 为基准。
//
// 三行的中线严格等距（5 / 11 / 17），上下各留 5：只要有一行偏 0.5，
// 缩到 22px 就会看出整块是歪的——这种歪说不出理由，但一眼就别扭。
//
// 线高定 1.6 而不是整数 2：菜单栏图标要的是「细而清楚」，
// 2px 在 Retina 上显得笨重，1.6 经抗锯齿后落在 1.5px 左右正合适。
var shapes = []shape{
	// 三个点，直径 3.2，比线粗——点和线有粗细对比才分得出层次。
	{cx: 3.4, cy: 5, hw: 1.6, hh: 1.6, r: 1.6},
	{cx: 3.4, cy: 11, hw: 1.6, hh: 1.6, r: 1.6},
	{cx: 3.4, cy: 17, hw: 1.6, hh: 1.6, r: 1.6},
	// 三条线。第三条只短一点：短太多会像被截断，反而显得没画完。
	{cx: 13.2, cy: 5, hw: 5.6, hh: 0.8, r: 0.8},
	{cx: 13.2, cy: 11, hw: 5.6, hh: 0.8, r: 0.8},
	{cx: 12.6, cy: 17, hw: 5.0, hh: 0.8, r: 0.8},
}

// DeskSize 是 Windows / Linux 托盘图的边长。
//
// 为什么不复用 Size：Windows 任务栏图标取 SM_CXSMICON，逻辑尺寸 16，
// 系统按 DPI 放大到 20 / 24 / 32。把 22 的图交给它缩到 16，边缘会糊；
// 48 是 16 / 24 的整数倍，32 也接近，四个档位下来都干净。
const DeskSize = 48

var (
	once    sync.Once
	light   []byte // 深色线条，给浅色菜单栏（macOS 的 template 图）
	dark    []byte // 浅色线条，给深色任务栏
	deskOn  []byte // 48×48 深色线条，给 Windows/Linux 浅色任务栏
	deskOff []byte // 48×48 浅色线条，给 Windows/Linux 深色任务栏
	encode  error
)

// Light 返回深色线条的 PNG。
//
// macOS 上不要直接用它——用 Template 让系统按深浅色自动反色。
func Light() ([]byte, error) {
	build()
	return light, encode
}

// Dark 返回浅色线条的 PNG，给深色托盘用。
func Dark() ([]byte, error) {
	build()
	return dark, encode
}

// DeskLight 返回 48×48 深色线条的 PNG：**浅色**任务栏 / 托盘用。
//
// 名字容易读反，所以说明白：返回的是「深色墨」，配浅色底。
// Windows 11 默认主题是浅色，任务栏白底，深色墨才看得见；
// 早先用浅色墨的版本在白底任务栏上等于隐形。
func DeskLight() ([]byte, error) {
	build()
	return deskOn, encode
}

// DeskDark 返回 48×48 浅色线条的 PNG：**深色**任务栏用。
//
// 和 DeskLight 成对交给 Wails（SetIcon + SetDarkModeIcon），
// 由它按系统主题切换。自己判断主题只会判断错——用户改主题时我们收不到通知。
func DeskDark() ([]byte, error) {
	build()
	return deskOff, encode
}

// Template 返回模板图：只有 alpha 有意义，颜色由系统决定。
//
// macOS 的 template icon 约定是纯黑加 alpha，系统在浅色菜单栏显示黑、
// 深色菜单栏显示白。用普通彩图的话，深色模式下会变成一团看不清的黑块。
func Template() ([]byte, error) {
	build()
	return template, encode
}

var template []byte

func build() {
	once.Do(func() {
		// 三份 22 的产物只有颜色不同，alpha 完全一样，所以先算一次 mask。
		small := coverage(Size)
		template, encode = encodePNG(small, color.NRGBA{0, 0, 0, 255})
		if encode != nil {
			return
		}
		light, encode = encodePNG(small, color.NRGBA{0, 0, 0, 255})
		if encode != nil {
			return
		}
		dark, encode = encodePNG(small, color.NRGBA{255, 255, 255, 255})
		if encode != nil {
			return
		}
		// 桌面托盘那两份单独算：尺寸不同，覆盖度得重采样。
		big := coverage(DeskSize)
		deskOn, encode = encodePNG(big, color.NRGBA{0, 0, 0, 255})
		if encode != nil {
			return
		}
		deskOff, encode = encodePNG(big, color.NRGBA{255, 255, 255, 255})
	})
}

// coverage 算出每个像素被图形盖住的比例，0~255。
//
// size 是输出边长，shapes 里的坐标永远以 Size 为基准，这里按比例放大采样点
// 再交回去判断——这样换尺寸不用维护第二份几何，粗细比例也就不会跑偏。
func coverage(size int) [][]uint8 {
	out := make([][]uint8, size)
	k := float64(size) / Size
	for py := 0; py < size; py++ {
		row := make([]uint8, size)
		for px := 0; px < size; px++ {
			hit := 0
			// 把这个像素切成 super×super 的小格，数有多少落在图形里。
			for sy := 0; sy < super; sy++ {
				for sx := 0; sx < super; sx++ {
					x := (float64(px) + (float64(sx)+0.5)/super) / k
					y := (float64(py) + (float64(sy)+0.5)/super) / k
					for _, s := range shapes {
						if s.inside(x, y) {
							hit++
							break
						}
					}
				}
			}
			row[px] = uint8(math.Round(float64(hit) * 255 / (super * super)))
		}
		out[py] = row
	}
	return out
}

// encodePNG 把覆盖度按给定颜色写成 PNG。边长从 mask 自己读，不另传。
func encodePNG(mask [][]uint8, c color.NRGBA) ([]byte, error) {
	h := len(mask)
	if h == 0 || len(mask[0]) != h {
		return nil, fmt.Errorf("覆盖度矩阵不是正方形")
	}
	img := image.NewNRGBA(image.Rect(0, 0, h, h))
	for y := 0; y < h; y++ {
		for x := 0; x < h; x++ {
			a := mask[y][x]
			// 图形本身是实心的，颜色不透明，只让边缘按覆盖度淡出。
			img.SetNRGBA(x, y, color.NRGBA{R: c.R, G: c.G, B: c.B, A: a})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("编码托盘图标失败: %w", err)
	}
	return buf.Bytes(), nil
}
