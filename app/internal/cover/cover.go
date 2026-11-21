// Package cover 生成发布用的封面图。
//
// 为什么要在程序里画：发一篇图文，图是绕不过去的一步。让用户为了一张
// 封面去开设计工具、导出、再找回来，这一步就足够让人放弃发布。
// 他要的往往只是「标题写大点，看着像回事」，那就在本地画出来。
//
// 这里刻意不接 AI 生图：封面要的是字大、对比强、一眼能读，
// 生成模型给的图经常字糊、比例不对，还得手动修。纯排版反而更快更稳，
// 而且离线、零成本、结果可预期——同样的标题永远得到同一张图。
package cover

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"unicode"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// Size 是封面的像素尺寸。
type Size struct {
	W, H int
}

// 各平台的默认尺寸。
//
// 小红书图文是 3:4 竖图（1080×1440 是平台上最常见的规格）；
// 公众号图文消息的封面按 2.35:1 展示在分享卡片里，用 900×383，
// 传 3:4 的图会被居中裁掉上下，标题就看不见了。
var (
	SizeXHS    = Size{1080, 1440}
	SizeWeChat = Size{900, 383}
)

// Style 是一套配色。深底浅字，不用白底：
// 白底封面在小红书的瀑布流里会被周围的图吞掉，划过去就没了。
type Style struct {
	Bg     color.RGBA // 背景
	Ink    color.RGBA // 标题
	Accent color.RGBA // 装饰条与副标题
}

var styles = []Style{
	{color.RGBA{0x1F, 0x29, 0x33, 0xFF}, color.RGBA{0xF6, 0xE7, 0xCB, 0xFF}, color.RGBA{0xE4, 0x57, 0x2E, 0xFF}},
	{color.RGBA{0x14, 0x21, 0x3D, 0xFF}, color.RGBA{0xFF, 0xFF, 0xFF, 0xFF}, color.RGBA{0xFC, 0xA3, 0x11, 0xFF}},
	{color.RGBA{0x2B, 0x2D, 0x42, 0xFF}, color.RGBA{0xED, 0xF2, 0xF4, 0xFF}, color.RGBA{0xEF, 0x23, 0x3C, 0xFF}},
	{color.RGBA{0x0B, 0x39, 0x54, 0xFF}, color.RGBA{0xF2, 0xF4, 0xF3, 0xFF}, color.RGBA{0xF9, 0xC7, 0x84, 0xFF}},
	{color.RGBA{0x3D, 0x40, 0x5B, 0xFF}, color.RGBA{0xF4, 0xF1, 0xDE, 0xFF}, color.RGBA{0xE0, 0x7A, 0x5F, 0xFF}},
	{color.RGBA{0x1B, 0x1B, 0x1B, 0xFF}, color.RGBA{0xFF, 0xF3, 0xE0, 0xFF}, color.RGBA{0x6F, 0xCF, 0x97, 0xFF}},
}

// Opt 是 Render 的入参。
type Opt struct {
	Title  string // 标题，必须非空
	Kicker string // 顶部小字，比如平台名或「第 12 期」，可空
	Foot   string // 底部小字，比如日期，可空
	Size   Size
	// Style 大于等于 0 时强制用这套配色；-1 表示按标题自动挑。
	Style int
}

// Render 画出一张封面。
//
// 只用「色块 + 大字」两样东西：
// 封面在信息流里只有一瞬间，能读到的只有标题本身；
// 加副标题、加图标、加边框，都是在跟那一瞬间抢注意力。
func Render(o Opt) (image.Image, error) {
	title := strings.TrimSpace(o.Title)
	if title == "" {
		return nil, fmt.Errorf("标题是空的，没法生成封面")
	}
	sz := o.Size
	if sz.W <= 0 || sz.H <= 0 {
		sz = SizeXHS
	}
	st := pickStyle(title, o.Style)

	img := image.NewRGBA(image.Rect(0, 0, sz.W, sz.H))
	fill(img, st.Bg)

	// 内边距按短边取比例：宽高比差很多时，固定像素会让一边挤一边空。
	// 横版（公众号分享卡片那种）短边是高，用 9.5% 只剩 36px，
	// 字会顶到边上去，所以横版多留一点。
	horizontal := float64(sz.W)/float64(sz.H) > 1.5
	padRatio := 0.095
	if horizontal {
		padRatio = 0.125
	}
	pad := float64(minInt(sz.W, sz.H)) * padRatio

	// 左侧一条竖线：位置固定、宽度按画布算，给文字一个"锚"。
	// 不加这条线时大段中文浮在色块上，边缘是散的。
	barW := float64(sz.W) * 0.007
	if barW < 4 {
		barW = 4
	}
	barX := pad
	gap := pad * 0.42
	textX := barX + barW + gap
	// 右侧留同样的 pad，左边多出来的是线宽和间隙——视觉上两边是齐的。
	textW := float64(sz.W) - pad - textX

	face, lines, lineH, err := fitTitle(title, textW, sz.H, horizontal)
	if err != nil {
		return nil, err
	}
	defer face.Close()

	// 标题块整体偏上：下方留白比上方多，视觉上像"标题压着画面"，
	// 居中会因为下半部空白显得标题在往下掉。
	blockH := float64(len(lines)) * lineH
	top := (float64(sz.H)-blockH)/2 - float64(sz.H)*0.045
	if top < pad+lineH {
		top = pad + lineH
	}

	drawRect(img, barX, top-lineH*0.62, barW, blockH+lineH*0.24, st.Accent)

	d := &font.Drawer{Dst: img, Src: image.NewUniform(st.Ink), Face: face}
	for i, ln := range lines {
		d.Dot = fixed.P(int(textX), int(top+float64(i)*lineH+ascentOf(face)))
		d.DrawString(ln)
	}

	// 顶部小字与底部小字：都只用来补一句上下文，字号压到很小，
	// 保证它们永远抢不走标题的注意力。
	small := float64(sz.H) * 0.021
	if small < 11 {
		small = 11
	}
	if s := strings.TrimSpace(o.Kicker); s != "" {
		if f, err := newFace(small); err == nil {
			sd := &font.Drawer{Dst: img, Src: image.NewUniform(st.Accent), Face: f}
			sd.Dot = fixed.P(int(textX), int(pad*0.82+ascentOf(f)))
			sd.DrawString(trimToWidth(s, f, textW))
			f.Close()
		}
	}
	if s := strings.TrimSpace(o.Foot); s != "" {
		if f, err := newFace(small); err == nil {
			sd := &font.Drawer{Dst: img, Src: image.NewUniform(st.Accent), Face: f}
			y := float64(sz.H) - pad*0.72
			sd.Dot = fixed.P(int(textX), int(y+ascentOf(f)))
			sd.DrawString(trimToWidth(s, f, textW))
			f.Close()
		}
	}
	return img, nil
}

// SavePNG 把图写到 path（自动建目录）。
func SavePNG(img image.Image, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

// Thumbnail 缩出一张宽 targetW 的 PNG，给界面预览用。
//
// 用 CatmullRom 而不是最近邻：默认缩放会把字边缘糊成锯齿，
// 用户看到一张毛糙的预览，会以为封面本身做得糙。
func Thumbnail(img image.Image, targetW int) []byte {
	b := img.Bounds()
	w := targetW
	if w <= 0 || w >= b.Dx() {
		w = b.Dx()
	}
	h := b.Dy() * w / b.Dx()
	if h < 1 {
		h = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, b, xdraw.Src, nil)
	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		return nil
	}
	return buf.Bytes()
}

// ---------- 排版 ----------

// fitTitle 求一个能「把标题撑满画布」的字号。
//
// 从大往小试，而不是先算再缩：中文没有空格，换行点由宽度决定，
// 字号一改行数就可能跳变，正着算容易算出一组"刚好差一个像素"的行。
//
// 起点放到画布高的 20%，不是为了填满整个画面——封面要留白，
// 但字号太小（早先用 8.8%）时，三行字只占画面上三分之一，
// 缩在信息流里根本读不到，等于白做。
func fitTitle(title string, textW float64, h int, horizontal bool) (font.Face, []string, float64, error) {
	ratio := 0.18
	maxLines := 4
	// 横版画布矮，行数一多字就小到读不清，只给两行。
	// 横版只出现在公众号分享卡片里，那里本来就该克制一点。
	if horizontal {
		maxLines = 2
	}
	size := float64(h) * ratio
	if size > 300 {
		size = 300
	}
	for size >= 26 {
		f, err := newFace(size)
		if err != nil {
			return nil, nil, 0, err
		}
		lines := wrapBalanced(title, f, textW)
		// 行高 1.34：中文没有下伸部，1.3 以下会显得挤；再大会松散。
		lh := size * 1.34
		// 最多 4 行、最高占画布 62%：超过就不是封面是海报了，
		// 而且平台缩略图里行数越多每行越小，读起来更费劲。
		if len(lines) <= maxLines && float64(len(lines))*lh <= float64(h)*0.62 {
			return f, lines, lh, nil
		}
		f.Close()
		size *= 0.92
	}
	return nil, nil, 0, fmt.Errorf("标题太长了（%d 字），缩短一点再生成封面", len([]rune(title)))
}

// wrapBalanced 断行，并且避免最后一行只剩一两个字。
//
// 「一个人也能跑通的自媒体工作台」按贪心断是「…工作 / 台」，
// 最后一行孤零零一个字，看着像排版坏了。这种"孤字"在中文排版里
// 是最刺眼的一种错，必须按行数重新均分。
func wrapBalanced(s string, f font.Face, maxW float64) []string {
	lines := wrap(s, f, maxW)
	if !lonelyTail(lines) {
		return lines
	}
	rs := []rune(strings.TrimSpace(s))
	for n := len(lines); n >= 2; n-- {
		if alt, ok := splitEven(rs, f, maxW, n); ok && !lonelyTail(alt) {
			return alt
		}
	}
	return lines
}

// lonelyTail 判断末行是不是明显比前一行短。
//
// 阈值不用「只有一两个字」，而是「不到前一行的一半」：
// 「…的自媒体 / 工作台」这种 12 比 3 在数学上不算孤字，
// 但在画面上是一眼可见的失衡，一样要重新均分。
func lonelyTail(lines []string) bool {
	n := len(lines)
	if n < 2 {
		return false
	}
	last := len([]rune(lines[n-1]))
	prev := len([]rune(lines[n-2]))
	return last*2 <= prev
}

// splitEven 把字平均分到 n 行，每行不超过 maxW；放不下就报 false。
func splitEven(rs []rune, f font.Face, maxW float64, n int) ([]string, bool) {
	if len(rs) == 0 || n < 1 {
		return nil, false
	}
	var out []string
	i := 0
	for k := 0; k < n && i < len(rs); k++ {
		// 余数往前几行摊，最后一行不至于被剩下的一大坨塞满。
		per := len(rs) / n
		if k < len(rs)%n {
			per++
		}
		end := i + per
		if end > len(rs) {
			end = len(rs)
		}
		// 别在英文单词中间切：往后推到词边界。
		for end < len(rs) && end > i && isWordRune(rs[end-1]) && isWordRune(rs[end]) {
			end++
		}
		seg := rs[i:end]
		var w float64
		for _, r := range seg {
			w += runeW(f, r)
		}
		if w > maxW {
			return nil, false
		}
		out = append(out, string(seg))
		i = end
	}
	if i < len(rs) {
		// 均分没摊完（英文词边界把行撑长了）：剩下的接在最后一行，
		// 这时宽度已经超了，交给外层缩字号。
		out[len(out)-1] += string(rs[i:])
		var w float64
		for _, r := range []rune(out[len(out)-1]) {
			w += runeW(f, r)
		}
		if w > maxW {
			return nil, false
		}
	}
	return out, true
}

// wrap 按字符断行。中文一个字就是一个单位，直接累积宽度即可；
// 英文单词不能被劈开，所以遇到长串字母数字时回退到词边界。
func wrap(s string, f font.Face, maxW float64) []string {
	var out []string
	var cur []rune
	var w float64
	flush := func() {
		if t := strings.TrimSpace(string(cur)); t != "" {
			out = append(out, t)
		}
		cur = cur[:0]
		w = 0
	}
	for _, r := range s {
		if r == '\n' {
			flush()
			continue
		}
		rw := runeW(f, r)
		if w+rw > maxW && len(cur) > 0 {
			// 遇到英文单词中间：把最后那个词挪到下一行，别把 "design" 切两半。
			if isWordRune(r) && isWordRune(cur[len(cur)-1]) {
				if i := lastWordStart(cur); i > 0 && i < len(cur) {
					tail := append([]rune(nil), cur[i:]...)
					cur = cur[:i]
					flush()
					cur = append(cur, tail...)
					w = 0
					for _, tr := range cur {
						w += runeW(f, tr)
					}
				} else {
					flush()
				}
			} else {
				flush()
			}
		}
		// 行首不能是标点：逗号、句号顶行会让整块文字看着歪。
		if len(cur) == 0 && isNoLineStart(r) && len(out) > 0 {
			// 把它挂到上一行末尾。
			prev := out[len(out)-1]
			out[len(out)-1] = prev + string(r)
			continue
		}
		cur = append(cur, r)
		w += rw
	}
	flush()
	if len(out) == 0 {
		out = []string{s}
	}
	return out
}

func isWordRune(r rune) bool {
	return r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_')
}

func lastWordStart(rs []rune) int {
	for i := len(rs) - 1; i >= 0; i-- {
		if !isWordRune(rs[i]) {
			return i + 1
		}
	}
	return 0
}

// 这些字符不能出现在行首（中文排版的「避头尾」）。
func isNoLineStart(r rune) bool {
	switch r {
	case '，', '。', '、', '；', '：', '？', '！', '）', '》', '」', '』', '”', '’', '…',
		',', '.', ';', ':', '?', '!', ')', ']', '}':
		return true
	}
	return false
}

func runeW(f font.Face, r rune) float64 {
	return float64(font.MeasureString(f, string(r))) / 64
}

func trimToWidth(s string, f font.Face, maxW float64) string {
	var w float64
	var b strings.Builder
	for _, r := range s {
		rw := runeW(f, r)
		if w+rw > maxW {
			return b.String() + "…"
		}
		b.WriteRune(r)
		w += rw
	}
	return b.String()
}

func ascentOf(f font.Face) float64 {
	return float64(f.Metrics().Ascent) / 64
}

// ---------- 绘制 ----------

func fill(img *image.RGBA, c color.RGBA) {
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i] = c.R
		img.Pix[i+1] = c.G
		img.Pix[i+2] = c.B
		img.Pix[i+3] = 0xFF
	}
}

func drawRect(img *image.RGBA, x, y, w, h float64, c color.RGBA) {
	if w <= 0 || h <= 0 {
		return
	}
	r := image.Rect(int(x), int(y), int(x+w), int(y+h)).Intersect(img.Bounds())
	for py := r.Min.Y; py < r.Max.Y; py++ {
		for px := r.Min.X; px < r.Max.X; px++ {
			img.SetRGBA(px, py, c)
		}
	}
}

// pickStyle 让同一个标题永远得到同一套配色：
// 用户改一版标题再生成，如果是另一套色，会以为程序乱来。
func pickStyle(title string, force int) Style {
	if force >= 0 && force < len(styles) {
		return styles[force]
	}
	var sum uint32
	for _, r := range title {
		sum = sum*31 + uint32(r)
	}
	return styles[int(sum%uint32(len(styles)))]
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ---------- 字体 ----------

var (
	fontOnce sync.Once
	theFont  *opentype.Font
	fontPath string
	fontErr  error
)

// FontPath 返回实际用上的字体文件，供日志与排查。
func FontPath() string {
	_, _ = pickFont()
	return fontPath
}

// 按优先级找中文字体。
//
// 不用 x/image 自带的 Go 字体：它没有汉字，标题会全变成方框。
// 系统字体是唯一能保证「任何汉字都能画出来」的来源，
// 而 Windows 与 macOS 的文件名完全不同，只能各排一份。
//
// macOS 上把黑体放前面：封面要的是粗、直、远看能认，
// 宋体那种细笔在缩略图里会糊成一团。
func candidateFonts() []string {
	switch runtime.GOOS {
	case "windows":
		root := os.Getenv("WINDIR")
		if root == "" {
			root = `C:\Windows`
		}
		return []string{
			filepath.Join(root, "Fonts", "msyhbd.ttc"),
			filepath.Join(root, "Fonts", "msyh.ttc"),
			filepath.Join(root, "Fonts", "simhei.ttf"),
			filepath.Join(root, "Fonts", "simsun.ttc"),
		}
	case "darwin":
		return []string{
			"/System/Library/Fonts/Hiragino Sans GB.ttc",
			"/System/Library/Fonts/PingFang.ttc",
			"/System/Library/Fonts/Supplemental/Songti.ttc",
			// 这两份黑体在部分 macOS 上用的是 x/image 读不了的 cmap 格式，
			// 排最后只是多一次尝试，读不了就会被跳过。
			"/System/Library/Fonts/STHeiti Medium.ttc",
			"/System/Library/Fonts/STHeiti Light.ttc",
		}
	default:
		return []string{
			"/usr/share/fonts/opentype/noto/NotoSansCJK-Bold.ttc",
			"/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc",
			"/usr/share/fonts/truetype/wqy/wqy-zenhei.ttc",
			"/usr/share/fonts/truetype/arphic/uming.ttc",
		}
	}
}

// font 找到并解析出一个能画汉字的字体，整个进程只做一次。
//
// 「能画汉字」必须实测，不能靠文件名猜：macOS 的 STHeiti 明明装着汉字，
// 但它的 cmap 用了 x/image 不支持的分段格式，解析出来 GlyphIndex 直接报错。
// 只看文件存在就选它，结果就是每个字都画不出来还一路成功。
func pickFont() (*opentype.Font, error) {
	fontOnce.Do(func() {
		var tried []string
		for _, p := range candidateFonts() {
			f, err := loadFont(p)
			if err == nil {
				theFont, fontPath = f, p
				return
			}
			tried = append(tried, err.Error())
		}
		fontErr = fmt.Errorf("本机没有能用来画封面的中文字体：%s", strings.Join(tried, "；"))
	})
	return theFont, fontErr
}

// loadFont 从单个文件里挑出第一个真能画汉字、且能正常取字形的成员。
func loadFont(path string) (*opentype.Font, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%s 读不到", filepath.Base(path))
	}
	if len(b) < 4096 {
		return nil, fmt.Errorf("%s 只有 %d 字节，不是字体", filepath.Base(path), len(b))
	}

	var fonts []*opentype.Font
	if isCollection(b) {
		// ttc 是字体集合，一个文件里有多个字重。直接 Parse 会失败，
		// 所以先按集合解析，再挑成员。
		c, err := opentype.ParseCollection(b)
		if err != nil {
			return nil, fmt.Errorf("%s 解析失败（%v）", filepath.Base(path), err)
		}
		for i := 0; i < c.NumFonts(); i++ {
			if f, err := c.Font(i); err == nil {
				fonts = append(fonts, f)
			}
		}
		if len(fonts) == 0 {
			return nil, fmt.Errorf("%s 里没有可用的字重", filepath.Base(path))
		}
	} else {
		f, err := opentype.Parse(b)
		if err != nil {
			return nil, fmt.Errorf("%s 解析失败（%v）", filepath.Base(path), err)
		}
		fonts = []*opentype.Font{f}
	}

	for _, f := range fonts {
		if canDrawHan(f) {
			return f, nil
		}
	}
	return nil, fmt.Errorf("%s 里没有能画汉字的字重", filepath.Base(path))
}

// canDrawHan 用几个必用到的字做实测：标题要先过一遍，
// 任何一个画不出来都说明这份字体不能用来做封面。
func canDrawHan(f *opentype.Font) bool {
	for _, r := range []rune{'中', '的', '一', '0', 'A'} {
		idx, err := f.GlyphIndex(nil, r)
		if err != nil || idx == 0 {
			return false
		}
	}
	return true
}

// newFace 按像素字号取一个 face。
//
// 调用方负责 Close，所以这里不缓存 face——缓存的是解析结果（最贵的部分），
// 而不是每个字号各留一份。
func newFace(size float64) (font.Face, error) {
	f, err := pickFont()
	if err != nil {
		return nil, err
	}
	return opentype.NewFace(f, &opentype.FaceOptions{
		Size:    size,
		DPI:     72, // 72 时 Size 就是像素高，省一次换算
		Hinting: font.HintingFull,
	})
}

// isCollection 判断是不是 ttc/otc 集合（魔数 'ttcf'）。
func isCollection(b []byte) bool {
	return len(b) >= 4 && b[0] == 't' && b[1] == 't' && b[2] == 'c' && b[3] == 'f'
}
