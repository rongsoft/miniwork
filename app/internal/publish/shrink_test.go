package publish

import (
	"context"
	"image"
	"image/png"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 这一组测试守的是「图太大」这条最常见的失败。
//
// 手机拍一张 4MB 的照片，微信正文配图接口只收 1MB 以下。如果不压缩，
// 用户看到的是 40005 / invalid image size 之类一句话，跟"图太大"毫无关系。
// 而压缩逻辑最容易写错的地方是"压了但没生效"——程序自己看不出来，
// 只有假服务端量到的字节数能证明。

// uploadBytes 取假服务端从某个路径收到的文件大小（文件名不可预知，按前缀找）。
func uploadBytes(f *fakeWX, path string) int64 {
	var n int64
	for k, v := range f.size {
		if strings.HasPrefix(k, path+"#") {
			n += v
		}
	}
	return n
}

// noisePNG 造一张噪点 PNG。噪点压不动，所以体积主要由像素量决定，
// 用它才能造出真实的"大图"，纯色图压完只有几 KB。
func noisePNG(t *testing.T, side int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, side, side))
	rnd := rand.New(rand.NewSource(7))
	for i := range img.Pix {
		img.Pix[i] = byte(rnd.Intn(256))
	}
	// 让 alpha 恒为不透明，否则 JPEG 编码会把它当透明处理。
	for i := 3; i < len(img.Pix); i += 4 {
		img.Pix[i] = 255
	}
	p := filepath.Join(t.TempDir(), "big.png")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	fi, _ := f.Stat()
	if fi.Size() <= maxContentImage {
		t.Fatalf("测试图没造够大：%d 字节", fi.Size())
	}
	return p
}

// TestContentImageShrunk 超过 1MB 的正文配图必须先压再传。
func TestContentImageShrunk(t *testing.T) {
	wx, f := newFakeWX(t)
	big := noisePNG(t, 1200)

	if _, err := wx.UploadContentImage(context.Background(), big); err != nil {
		t.Fatalf("上传失败: %v", err)
	}
	got := uploadBytes(f, "/cgi-bin/media/uploadimg")
	if got == 0 {
		t.Fatal("假服务端没收到文件，上传根本没发生")
	}
	if got > maxContentImage {
		t.Fatalf("传上去的还是 %s，超过 1MB 限制——压缩没生效", byteSize(got))
	}
	t.Logf("原图 %s → 上传 %s", byteSize(fileSize(t, big)), byteSize(got))
}

// TestSmallImageUntouched 本来就合规的图不该被重编码，画质一分都不能丢。
func TestSmallImageUntouched(t *testing.T) {
	wx, f := newFakeWX(t)
	small := tempImage(t, "small.png") // 一百来个字节的假 PNG 头

	if _, err := wx.UploadContentImage(context.Background(), small); err != nil {
		t.Fatalf("上传失败: %v", err)
	}
	if got := uploadBytes(f, "/cgi-bin/media/uploadimg"); got != fileSize(t, small) {
		t.Fatalf("小图被改动了：原 %d 字节，传上去 %d 字节", fileSize(t, small), got)
	}
}

// TestUnsupportedFormatTooBig 解不开的格式（webp / heic）要说清该怎么办。
func TestUnsupportedFormatTooBig(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "shot.webp")
	if err := os.WriteFile(p, make([]byte, maxContentImage+1024), 0o644); err != nil {
		t.Fatal(err)
	}
	wx, _ := newFakeWX(t)
	_, err := wx.UploadContentImage(context.Background(), p)
	if err == nil {
		t.Fatal("解不开的超大图应当报错")
	}
	if !strings.Contains(err.Error(), "webp") || !strings.Contains(err.Error(), "jpg") {
		t.Fatalf("错误里要指出格式并给出路：%v", err)
	}
}

func fileSize(t *testing.T, p string) int64 {
	t.Helper()
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	return fi.Size()
}
