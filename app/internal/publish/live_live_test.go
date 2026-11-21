//go:build live

package publish

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// 这一组测试会真的启动浏览器，所以单独一个 build tag。
//
// 跑法（别用整包，会带着其他用例一起起浏览器）：
//
//	go test -tags live ./internal/publish/ -run TestLive -count=1 -v
//
// 验的是「内嵌那一屏」两条最要紧的路：画面能推回来、点击能投进去。
// 这两条断了，界面看起来完全正常——一片白或者点了没反应——最难查。

// liveEngine 起一个能跑测试的引擎。headless 起，不占屏幕。
func liveEngine(t *testing.T) *Engine {
	t.Helper()
	e := New(Options{
		DataDir:   t.TempDir(),
		Headless:  true,
		NoSandbox: true,
	})
	t.Cleanup(e.Close)
	if _, err := e.BrowserPath(); err != nil {
		t.Skipf("这台机器上没有可驱动的浏览器: %v", err)
	}
	return e
}

// waitFrame 等第一帧画面。等不到就是推流没起来。
func waitFrame(t *testing.T, ch <-chan Frame, d time.Duration) Frame {
	t.Helper()
	select {
	case f := <-ch:
		return f
	case <-time.After(d):
		t.Fatalf("%s 内没收到任何画面帧——screencast 没起来", d)
		return Frame{}
	}
}

// TestLiveFramesAndClick 走完整一条：接画面 → 看到帧 → 点一下 → 页面有反应。
func TestLiveFramesAndClick(t *testing.T) {
	// 一个只有一颗大按钮的页面：点中就把 hash 改掉。
	// 用 hash 而不是别的方式，是因为它读回来的成本最低，不依赖渲染。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html><html><body style="margin:0">
<button id="b" style="position:absolute;left:0;top:0;width:300px;height:120px"
        onclick="location.hash='clicked'">hit</button>
<div style="position:absolute;left:0;top:140px">ready</div>
</body></html>`))
	}))
	defer srv.Close()

	e := liveEngine(t)
	frames := make(chan Frame, 8)
	e.SetFrameHandler(func(f Frame) {
		select {
		case frames <- f:
		default:
		}
	})

	if err := e.LiveResize("xiaohongshu", 800, 600); err != nil {
		t.Fatalf("设视口失败: %v", err)
	}
	if err := e.LiveOpen("xiaohongshu", srv.URL); err != nil {
		t.Fatalf("接画面失败: %v", err)
	}

	f := waitFrame(t, frames, 20*time.Second)
	if len(f.Data) < 500 {
		t.Fatalf("第一帧只有 %d 字节，不像一张真的画面", len(f.Data))
	}
	if f.W != 800 || f.H != 600 {
		t.Errorf("帧报告的视口是 %dx%d，想要 800x600——视口没生效画面会糊", f.W, f.H)
	}

	// 页面还在加载时点会丢，等它把按钮画出来。
	deadline := time.Now().Add(15 * time.Second)
	for {
		ok, err := e.LiveEval("xiaohongshu", `!!document.getElementById('b')`)
		if err == nil && ok == "true" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("页面里的按钮一直没出现: ok=%q err=%v", ok, err)
		}
		time.Sleep(200 * time.Millisecond)
	}

	// 点按钮正中间。坐标是 CSS 像素——视口就是按 CSS 像素设的。
	if err := e.LiveInput("xiaohongshu", LiveInput{Kind: "click", X: 150, Y: 60}); err != nil {
		t.Fatalf("投点击失败: %v", err)
	}

	deadline = time.Now().Add(8 * time.Second)
	for {
		hash, err := e.LiveEval("xiaohongshu", "location.hash")
		if err == nil && hash == "#clicked" {
			return // 通了
		}
		if time.Now().After(deadline) {
			t.Fatalf("点了画面上的按钮，页面没反应：hash=%q err=%v", hash, err)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// TestLiveTextReachesPage 验打字：输入的文字要真的进到页面的输入框里。
//
// 中文输入法在界面上走 composition 事件，最终发的是「几个字」这么一段，
// 落到页面用的是 InsertText——这条通不通，决定用户敢不敢在网页里改点东西。
func TestLiveTextReachesPage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html><html><body style="margin:0">
<textarea id="t" style="position:absolute;left:0;top:0;width:600px;height:200px"></textarea>
</body></html>`))
	}))
	defer srv.Close()

	e := liveEngine(t)
	e.SetFrameHandler(func(Frame) {})
	if err := e.LiveResize("xiaohongshu", 800, 600); err != nil {
		t.Fatalf("设视口失败: %v", err)
	}
	if err := e.LiveOpen("xiaohongshu", srv.URL); err != nil {
		t.Fatalf("接画面失败: %v", err)
	}

	deadline := time.Now().Add(15 * time.Second)
	for {
		ok, err := e.LiveEval("xiaohongshu", `!!document.getElementById('t')`)
		if err == nil && ok == "true" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("输入框一直没出现: ok=%q err=%v", ok, err)
		}
		time.Sleep(200 * time.Millisecond)
	}

	// 先把焦点点进去，再打字。
	if err := e.LiveInput("xiaohongshu", LiveInput{Kind: "click", X: 100, Y: 60}); err != nil {
		t.Fatalf("投点击失败: %v", err)
	}
	time.Sleep(300 * time.Millisecond)
	if err := e.LiveInput("xiaohongshu", LiveInput{Kind: "text", Text: "今天发什么"}); err != nil {
		t.Fatalf("投文字失败: %v", err)
	}

	deadline = time.Now().Add(8 * time.Second)
	for {
		v, err := e.LiveEval("xiaohongshu", "document.getElementById('t').value")
		if err == nil && v == "今天发什么" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("输入框里没有打进去的字：value=%q err=%v", v, err)
		}
		time.Sleep(200 * time.Millisecond)
	}
}
