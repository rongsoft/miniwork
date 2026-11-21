package main

import (
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"image"
	// 用户选的图什么格式都有，解码器得先注册上，否则 image.Decode
	// 只认得 png（cover 包里 import 的那个），jpg 会全部返回失败。
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"
	"time"

	"rongsoft/miniwork/app/internal/cover"
)

// CoverView 是生成好的一张封面。
//
// 同时给路径和 dataURL 两份：
//   - Path 发布时用，浏览器上传要的是磁盘上的文件；
//   - Thumb 界面预览用，dataURL 不用另开一个静态文件服务，
//     也不用担心 WebView 能不能读 file://。
//
// 只给路径的话，用户点了「生成封面」什么也看不见——他得自己
// 去 Finder 里找到那个文件才能确认生成了什么，这跟"顺畅"是反的。
type CoverView struct {
	Path  string `json:"path"`
	Thumb string `json:"thumb"`
	W     int    `json:"w"`
	H     int    `json:"h"`
}

// PreviewImages 把本地图片读成缩略 dataURL，给界面显示「我到底选了哪张」。
//
// 不带这个的话，发布面板只能显示「已选 2 张」——用户在发布前
// 没法确认自己选的图和稿子对不对，而发出去就收不回来了。
//
// 读不出来（不是图片、文件被移走）就返回空串占位，不整体报错：
// 一张图坏了不该让另外八张也看不见。
func (s *TopicService) PreviewImages(paths []string) []string {
	out := make([]string, 0, len(paths))
	for i, p := range paths {
		if i >= 9 {
			break
		}
		p = strings.TrimSpace(p)
		if p == "" {
			out = append(out, "")
			continue
		}
		f, err := os.Open(p)
		if err != nil {
			out = append(out, "")
			continue
		}
		img, _, err := image.Decode(f)
		f.Close()
		if err != nil {
			out = append(out, "")
			continue
		}
		b := cover.Thumbnail(img, 240)
		if len(b) == 0 {
			out = append(out, "")
			continue
		}
		out = append(out, "data:image/png;base64,"+base64.StdEncoding.EncodeToString(b))
	}
	return out
}

// GenerateCover 按标题画一张封面。
//
// platform 决定尺寸：小红书是 3:4 竖图，公众号分享卡片是 2.35:1 横图，
// 传错比例平台会裁掉上下，标题就看不见了。
func (s *TopicService) GenerateCover(title, platform, kicker string) (CoverView, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return CoverView{}, fmt.Errorf("这一稿还没有标题，先写个标题再生成封面")
	}

	size := cover.SizeXHS
	if platform == "wechat" {
		size = cover.SizeWeChat
	}

	img, err := cover.Render(cover.Opt{
		Title:  title,
		Kicker: strings.TrimSpace(kicker),
		Foot:   time.Now().Format("2006.01.02"),
		Size:   size,
		Style:  -1,
	})
	if err != nil {
		return CoverView{}, err
	}

	// 文件名用内容哈希：同一个标题重复点「生成封面」得到同一个文件，
	// 不会在 covers 目录里堆出一串废图。
	sum := sha1.Sum([]byte(platform + "|" + title))
	name := hex.EncodeToString(sum[:8]) + ".png"
	path := filepath.Join(filepath.Dir(s.dbPath), "covers", name)
	if err := cover.SavePNG(img, path); err != nil {
		return CoverView{}, fmt.Errorf("封面存不下来：%w", err)
	}

	// 缩略图再缩一半：1080×1440 的 PNG 转 base64 有 100KB+，
	// 每次生成都往界面塞这么大一坨没必要——预览只要看清版式。
	thumb := cover.Thumbnail(img, 540)

	return CoverView{
		Path:  path,
		Thumb: "data:image/png;base64," + base64.StdEncoding.EncodeToString(thumb),
		W:     size.W,
		H:     size.H,
	}, nil
}
