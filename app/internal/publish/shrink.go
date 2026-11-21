package publish

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"
)

// 微信对上传图片有两个不同的上限，而且都不宽容：
//
//	正文配图（media/uploadimg）只收 1MB 以下的 jpg/png
//	永久素材（封面、贴图）收 10MB 以下的 png/jpeg/gif
//
// 手机拍的照片普遍 3-5MB，直接传必然被拒。更麻烦的是拒回来的错误码
// （40005 / 45009 / invalid image size）跟"图片太大"没有半点字面关系，
// 用户看到只会计一句"这软件不行"。
//
// 所以上传前先量一下，超限就在本地重编码到限制内：宁可压掉一点画质，
// 也不要把一句看不懂的错误丢给用户。压不下去才报错，并说清楚为什么。
const (
	maxContentImage  = 1 << 20  // 1MB：正文配图
	maxMaterialImage = 10 << 20 // 10MB：永久素材
)

// fitImage 保证图片能过微信的体积和格式要求。
//
// 返回的路径可能是一个新建的临时文件——此时 cleanup 不是 nil，调用方必须执行。
func fitImage(path string, max int64) (out string, cleanup func(), err error) {
	fi, err := os.Stat(path)
	if err != nil {
		return "", nil, fmt.Errorf("读不到图片 %s: %w", filepath.Base(path), err)
	}
	ext := strings.ToLower(filepath.Ext(path))
	// 微信只认这两种正文格式；已经合规又没超限就原样传，不折腾画质。
	if fi.Size() <= max && (ext == ".jpg" || ext == ".jpeg" || ext == ".png") {
		return path, nil, nil
	}

	data, err := shrinkImage(path, max)
	if err != nil {
		return "", nil, err
	}

	f, err := os.CreateTemp("", "miniwk-*.jpg")
	if err != nil {
		return "", nil, err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", nil, err
	}
	f.Close()
	return f.Name(), func() { os.Remove(f.Name()) }, nil
}

// shrinkImage 把图压到 max 字节以内。
//
// 先降质量再缩边：缩边是不可逆的画质损失，而质量从 85 降到 75
// 肉眼看不出、体积却能掉一半，应该先花这份便宜。
func shrinkImage(path string, max int64) ([]byte, error) {
	src, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer src.Close()

	img, format, err := image.Decode(src)
	if err != nil {
		// 解不开的格式（webp / heic / 相机原生）没法在本地压。
		// 这时候只剩一条路：让用户自己换一张，所以话要说清楚。
		fi, _ := src.Stat()
		return nil, fmt.Errorf("%s 是 %s 格式（%.1fMB），微信不收这种格式。转成 jpg 再发",
			filepath.Base(path), guessFormat(path), mb(fi.Size()))
	}

	scale := 1.0
	for attempt := 0; attempt < 8; attempt++ {
		cur := scaleImage(img, scale)
		for _, q := range []int{85, 78, 70, 60} {
			var buf bytes.Buffer
			if err := jpeg.Encode(&buf, cur, &jpeg.Options{Quality: q}); err != nil {
				return nil, err
			}
			if int64(buf.Len()) <= max {
				return buf.Bytes(), nil
			}
		}
		scale *= 0.75
	}
	return nil, fmt.Errorf("%s（原图 %s 格式）压不到 %s 以内，换一张小一点的图",
		filepath.Base(path), format, byteSize(max))
}

// scaleImage 按比例缩边。scale>=1 时原样返回。
func scaleImage(img image.Image, scale float64) image.Image {
	if scale >= 1 {
		return img
	}
	b := img.Bounds()
	w := int(float64(b.Dx()) * scale)
	if w < 1 {
		w = 1
	}
	return resample(img, w)
}

// resample 用最近邻缩放。
//
// 不用双三次：公众号正文图是给人看小图的，最近邻缩一半几乎看不出来，
// 而它不需要引入任何图像库、也不会在小图上卡 CPU。
func resample(src image.Image, width int) image.Image {
	b := src.Bounds()
	h := int(float64(b.Dy()) * float64(width) / float64(b.Dx()))
	if h < 1 {
		h = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, width, h))
	for y := 0; y < h; y++ {
		sy := b.Min.Y + y*b.Dy()/h
		for x := 0; x < width; x++ {
			sx := b.Min.X + x*b.Dx()/width
			dst.Set(x, y, src.At(sx, sy))
		}
	}
	return dst
}

func guessFormat(path string) string {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
	if ext == "" {
		return "不认识的"
	}
	return ext
}

func mb(n int64) float64 { return float64(n) / (1 << 20) }

func byteSize(n int64) string {
	if n >= 1<<20 {
		return fmt.Sprintf("%.0fMB", mb(n))
	}
	return fmt.Sprintf("%.0fKB", float64(n)/(1<<10))
}
