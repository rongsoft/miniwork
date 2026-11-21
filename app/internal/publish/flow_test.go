package publish

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"

	_ "modernc.org/sqlite"
)

// 这些测试用本地页面模拟平台发布页，不需要登录、不联网。
//
// 为什么值得写：真正的发布选择器一定会随平台改版失效，而「失效时
// 会怎么样」比「现在能不能用」更重要——它必须是「停在浏览器里等人接手」，
// 不能是静默失败，也不能白等两分钟。这套逻辑只有真驱动一次浏览器才验得出来。
//
// 没有浏览器时自动跳过（CI 上常见），不算失败。

// testBrowser 返回测试用的浏览器路径；没有就跳过。
func testBrowser(t *testing.T) string {
	t.Helper()
	if p := strings.TrimSpace(os.Getenv("MW_TEST_BROWSER")); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	for _, cand := range append(browserCandidates(), playwrightChromium()) {
		if _, err := os.Stat(cand); err == nil {
			return cand
		}
	}
	t.Skip("本机没有可驱动的浏览器，跳过发布引擎测试")
	return ""
}

// playwrightChromium 是本机调试用的一份 Chromium（装了 playwright 才有）。
func playwrightChromium() string {
	base := filepath.Join(os.Getenv("HOME"), "Library/Caches/ms-playwright")
	matches, _ := filepath.Glob(filepath.Join(base, "chromium-*", "chrome-mac*", "Google Chrome for Testing.app", "Contents", "MacOS", "Google Chrome for Testing"))
	if len(matches) > 0 {
		return matches[0]
	}
	return ""
}

// fixture 造一个假发布页，返回 file:// 地址和图片路径。
//
// moveOn 决定点完「发布」之后跳不跳走：
//   - true 模拟发布成功（真实平台会跳到文章页/列表页）
//   - false 模拟被拦（原地弹个提示，URL 一动不动）——这是最容易被
//     误报成成功的情形，必须有专门的用例守住
func fixture(t *testing.T, moveOn bool) (pageURL string, images []string) {
	t.Helper()
	dir := t.TempDir()

	// 跳走的时候把填进去的内容带在 URL 上：跳走之后编辑页已经销毁，
	// 测试只能从新页面读回"提交那一刻到底填了什么"。
	nav := ""
	if moveOn {
		nav = `location.href = 'success.html'
      + '?title=' + encodeURIComponent(document.getElementById('title').value)
      + '&body=' + encodeURIComponent(document.getElementById('body').innerText)
      + '&files=' + window.__files;`
	}
	html := `<!doctype html><html><head><meta charset="utf-8"><title>fixture</title></head>
<body>
  <input id="file" type="file" accept="image/*" multiple>
  <input id="title" type="text" placeholder="填写标题">
  <div id="body" contenteditable="true" data-placeholder="正文"></div>
  <button id="pub">发布</button>
  <div id="err" hidden>还有必填项没填</div>
<script>
  window.__files = 0;
  window.__submitted = false;
  document.getElementById('file').addEventListener('change', function (e) {
    window.__files = e.target.files.length;
  });
  document.getElementById('pub').addEventListener('click', function () {
    window.__submitted = true;
    ` + nav + `
    document.getElementById('err').hidden = false;
  });
</script>
</body></html>`

	p := filepath.Join(dir, "publish.html")
	if err := os.WriteFile(p, []byte(html), 0o644); err != nil {
		t.Fatalf("写测试页失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "success.html"), []byte("<html><body>发布成功</body></html>"), 0o644); err != nil {
		t.Fatalf("写成功页失败: %v", err)
	}

	// 最小的合法 PNG（1x1 透明），够 CDP 上传用。
	png := []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
		0x89, 0x00, 0x00, 0x00, 0x0a, 0x49, 0x44, 0x41,
		0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
		0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00,
		0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae,
		0x42, 0x60, 0x82,
	}
	img := filepath.Join(dir, "a.png")
	if err := os.WriteFile(img, png, 0o644); err != nil {
		t.Fatalf("写测试图片失败: %v", err)
	}
	return "file://" + p, []string{img}
}

func newEngine(t *testing.T) *Engine {
	t.Helper()
	return New(Options{
		DataDir:  t.TempDir(),
		ExecPath: testBrowser(t),
		Headless: true,
		// 测试用的是临时 profile + 本地假页面，沙箱在这里没有保护对象；
		// 而容器/受限环境里沙箱常常起不来（表现是渲染进程一路 crash）。
		NoSandbox: true,
		Logger:    func(msg string) { t.Log("  · " + msg) },
	})
}

func fixturePlatform(pageURL string) Platform {
	return Platform{
		Key: "fixture", Name: "测试平台", Media: MediaImages,
		PublishURL: pageURL,
		Upload:     []string{`input[type=file][accept*="image"]`},
		Title:      []string{`input[placeholder*="标题"]`},
		Body:       []string{`div[contenteditable="true"]`},
		Submit:     []string{"发布"},
		EditorPath: "/publish.html",
	}
}

// evalJSON 在页面里跑一段表达式，结果按 JSON 解到 out。
func evalJSON(ctx context.Context, js string, out any) error {
	return chromedp.Run(ctx, chromedp.Evaluate(js, out))
}

// TestPublishOnFixture 走完整条链路：打开发布页 → 上传 → 填标题正文 → 点发布。
func TestPublishOnFixture(t *testing.T) {
	if testing.Short() {
		t.Skip("短模式跳过浏览器测试")
	}
	page, imgs := fixture(t, true)
	e := newEngine(t)
	defer e.Close()

	res, err := e.publishOn(fixturePlatform(page), Request{
		Title:      "一个人也能跑通的自媒体工作台",
		Body:       "先把选题攒起来，再挑一条写成稿。",
		Tags:       []string{"效率工具", "自媒体"},
		Images:     imgs,
		AutoSubmit: true,
	})
	if err != nil {
		t.Fatalf("发布失败: %v", err)
	}
	if !res.OK || res.Manual {
		t.Fatalf("应当自动发完，实际 %+v", res)
	}

	// 核对提交那一刻填进去的东西。发布成功后页面已经跳到 success.html，
	// 所以这些值是从它 URL 上带过来的——编辑页早就销毁了。
	var state struct {
		Title string `json:"title"`
		Body  string `json:"body"`
		Files int    `json:"files"`
	}
	js := `(function(){
      var p = new URLSearchParams(location.search);
      return {title: p.get('title') || '', body: p.get('body') || '', files: Number(p.get('files') || 0)};
    })()`
	ctx, err := e.ensure("fixture")
	if err != nil {
		t.Fatalf("取会话失败: %v", err)
	}
	if err := evalJSON(ctx, js, &state); err != nil {
		t.Fatalf("读提交结果失败: %v", err)
	}

	if !strings.Contains(state.Title, "自媒体工作台") {
		t.Errorf("标题没填上: %q", state.Title)
	}
	if !strings.Contains(state.Body, "先把选题攒起来") {
		t.Errorf("正文没填上: %q", state.Body)
	}
	// 标签按平台规则拼在正文末尾，和「复制全部」同一份规则。
	if !strings.Contains(state.Body, "#效率工具") {
		t.Errorf("标签没拼进正文: %q", state.Body)
	}
	if state.Files != 1 {
		t.Errorf("文件没传上去，页面只收到 %d 个", state.Files)
	}
	// 跳到了成功页 = 发布按钮真的点到了。这一步替代了原来的 __submitted 标志，
	// 因为提交后编辑页已经不存在，标志位读不回来了。
	var landed string
	_ = chromedp.Run(ctx, chromedp.Location(&landed))
	if !strings.Contains(landed, "success.html") {
		t.Errorf("点完发布没跳走，停在 %s", landed)
	}
}

// TestPublishStopsBeforeSubmit 验证默认行为：填完就停，最后一下留给用户。
func TestPublishStopsBeforeSubmit(t *testing.T) {
	if testing.Short() {
		t.Skip("短模式跳过浏览器测试")
	}
	page, imgs := fixture(t, true)
	e := newEngine(t)
	defer e.Close()

	res, err := e.publishOn(fixturePlatform(page), Request{
		Title:  "填完就停",
		Body:   "正文",
		Images: imgs,
	})
	if err != nil {
		t.Fatalf("流程报错: %v", err)
	}
	if !res.Manual || res.OK {
		t.Fatalf("应当停在等用户点发布，实际 %+v", res)
	}

	ctx, _ := e.ensure("fixture")
	var submitted bool
	if err := evalJSON(ctx, `window.__submitted`, &submitted); err != nil {
		t.Fatalf("读状态失败: %v", err)
	}
	if submitted {
		t.Error("不该替用户点发布")
	}
}

// TestSubmitWithoutLeavingIsManual 点过发布、但页面没动，不能记成成功。
//
// 这是最危险的一种"看起来成功"：按钮点到了、页面上什么也没报错，
// 只是弹了个红字提示，URL 一点没变。如果这里判成 OK，台账上会多一条
// 不存在的笔记，而用户要到粉丝说"没看到"的时候才发现。
func TestSubmitWithoutLeavingIsManual(t *testing.T) {
	if testing.Short() {
		t.Skip("短模式跳过浏览器测试")
	}
	page, imgs := fixture(t, false) // 不跳转 = 被拦
	e := newEngine(t)
	defer e.Close()

	res, err := e.publishOn(fixturePlatform(page), Request{
		Title:      "点了发布但被拦住",
		Body:       "正文",
		Images:     imgs,
		AutoSubmit: true,
	})
	if err != nil {
		t.Fatalf("流程报错: %v", err)
	}
	if res.OK {
		t.Fatalf("还停在编辑页，不该判成功：%+v", res)
	}
	if !res.Manual {
		t.Fatalf("应当是「等你接手」，实际 %+v", res)
	}
	// 卡住的理由要说人话，不能只说"失败"。
	if !strings.Contains(res.Reason, "编辑页") {
		t.Errorf("理由不够具体: %q", res.Reason)
	}
}

// TestMissingMediaRejected 缺素材要在开浏览器之前就拦住。
func TestMissingMediaRejected(t *testing.T) {
	e := New(Options{DataDir: t.TempDir(), Headless: true})
	p, ok := Get("xiaohongshu")
	if !ok {
		t.Fatal("平台表里没有小红书")
	}
	_, err := e.publishOn(p, Request{Title: "没有配图"})
	if err == nil || !strings.Contains(err.Error(), "必须带图") {
		t.Fatalf("应当提示要配图，实际 %v", err)
	}
	// 报错里不能出现 os.Stat 这种内部细节。
	if strings.Contains(err.Error(), "no such file") {
		t.Errorf("错误信息泄露了内部细节: %v", err)
	}
}

// TestSubmittedFileMissing 选中的文件被删了，要说清是哪个文件。
func TestSubmittedFileMissing(t *testing.T) {
	e := New(Options{DataDir: t.TempDir(), Headless: true})
	p, _ := Get("xiaohongshu")
	_, err := e.publishOn(p, Request{Title: "标题", Images: []string{"/tmp/不存在的图.png"}})
	if err == nil || !strings.Contains(err.Error(), "不存在的图.png") {
		t.Fatalf("应当指出具体哪个文件找不到，实际 %v", err)
	}
}

// TestNoBrowser 没有浏览器时给出的是可行动的错，而不是底层报错。
//
// 浏览器现在是随包发的，所以走到这条分支只有两种可能：包不完整，
// 或者机器上一份浏览器都没有。两种情况用户能做的事不同，
// 但都不该看到 os.Stat 的 "no such file or directory"。
func TestNoBrowser(t *testing.T) {
	e := New(Options{DataDir: t.TempDir(), ExecPath: "/definitely/not/here"})
	_, err := e.BrowserPath()
	if err == nil {
		t.Skip("本机有可用浏览器，跳过")
	}
	if !errors.Is(err, ErrNoBrowser) {
		t.Fatalf("应该是 ErrNoBrowser，实际 %v", err)
	}
	if strings.Contains(err.Error(), "no such file") {
		t.Fatalf("把系统报错原样丢给用户了：%v", err)
	}
	// 这句话要能让用户判断下一步：是不是装漏了。
	if !strings.Contains(err.Error(), "安装包") {
		t.Fatalf("错误信息该指到「安装包不完整」这条路上，实际 %v", err)
	}
}

// TestAuthedByProfileCookie 登录态是从 profile 里的 cookie 判的。
//
// 这条守的是「扫一次码，以后不用再扫」这个承诺。判错的两种后果都很烦：
// 认错了 → 到发布那步才报没登录；认漏了 → 每次都让用户重扫。
// 而真实登录态没法在测试里造（要真扫码），所以这里直接按 Chrome 的
// 磁盘格式造一个 cookies 库——它验证的是"路径找得对、查询写得对"。
func TestAuthedByProfileCookie(t *testing.T) {
	dir := t.TempDir()
	e := New(Options{DataDir: dir, ExecPath: testBrowser(t), Headless: true, NoSandbox: true})

	if e.Authed("xiaohongshu") {
		t.Fatal("连 profile 都没有，却说已登录")
	}

	// Chrome 的 cookie 库在 Default/Network/Cookies（新版）。
	netDir := filepath.Join(dir, "browser", "xiaohongshu", "Default", "Network")
	if err := os.MkdirAll(netDir, 0o755); err != nil {
		t.Fatal(err)
	}
	db := filepath.Join(netDir, "Cookies")
	conn, err := sql.Open("sqlite", "file:"+db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`CREATE TABLE cookies(name TEXT, value TEXT, host_key TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`INSERT INTO cookies VALUES('web_session','token','.xiaohongshu.com')`); err != nil {
		t.Fatal(err)
	}
	conn.Close()

	if !e.Authed("xiaohongshu") {
		t.Fatal("profile 里有登录 cookie，却判成未登录——用户扫完码还得再扫一次")
	}
	// 别的平台不该被这条 cookie 带成已登录。
	if e.Authed("douyin") {
		t.Error("抖音没有 profile，却因为小红书登录被判成已登录")
	}
}
