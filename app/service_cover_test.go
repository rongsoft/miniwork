package main

import (
	"os"
	"strings"
	"testing"
)

// 生成封面必须真的落一张文件：发布时浏览器上传的是路径，
// 只返回 dataURL 的话界面能预览、点发布却报"文件不存在"。
func TestGenerateCoverWritesFile(t *testing.T) {
	svc := newSvc(t)
	v, err := svc.GenerateCover("一个人也能跑通的自媒体工作台", "xiaohongshu", "小红书 · 图文")
	if err != nil {
		t.Fatal(err)
	}
	if v.W != 1080 || v.H != 1440 {
		t.Fatalf("小红书封面尺寸 %dx%d，应该是 1080x1440", v.W, v.H)
	}
	st, err := os.Stat(v.Path)
	if err != nil {
		t.Fatalf("文件没写出来：%v", err)
	}
	if st.Size() < 5_000 {
		t.Fatalf("只有 %d 字节，多半是空图", st.Size())
	}
	if !strings.HasPrefix(v.Thumb, "data:image/png;base64,") {
		t.Fatalf("预览不是 dataURL：%.40s", v.Thumb)
	}
	if len(v.Thumb) > 300_000 {
		t.Fatalf("预览图 %d 字节，太大了，界面会卡", len(v.Thumb))
	}
}

// 公众号分享卡片的比例是 2.35:1。给一张 3:4 竖图，平台会居中裁掉上下，
// 标题就只剩中间一小截——这是最容易返工的一种错。
func TestGenerateCoverWeChatSize(t *testing.T) {
	svc := newSvc(t)
	v, err := svc.GenerateCover("标题", "wechat", "公众号")
	if err != nil {
		t.Fatal(err)
	}
	if v.W != 900 || v.H != 383 {
		t.Fatalf("公众号封面 %dx%d，应该是 900x383", v.W, v.H)
	}
}

func TestGenerateCoverEmptyTitle(t *testing.T) {
	svc := newSvc(t)
	if _, err := svc.GenerateCover("   ", "xiaohongshu", ""); err == nil {
		t.Fatal("没标题必须报错，不能画一张只有背景的空图")
	}
}

// 同一个标题反复点生成，不该在 covers 目录里堆一堆文件。
func TestGenerateCoverReusesPath(t *testing.T) {
	svc := newSvc(t)
	a, err := svc.GenerateCover("同一个标题", "xiaohongshu", "")
	if err != nil {
		t.Fatal(err)
	}
	b, err := svc.GenerateCover("同一个标题", "xiaohongshu", "")
	if err != nil {
		t.Fatal(err)
	}
	if a.Path != b.Path {
		t.Fatalf("同一标题两次生成了两个文件：%s / %s", a.Path, b.Path)
	}
}
