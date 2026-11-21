//go:build browsercheck

package publish

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// 这个测试只在手动跑时执行：它会真的启动一个浏览器窗口（headless），
// 需要机器上已经有 browser/<平台>/ 那一份。
//
//	go test -tags browsercheck -run TestStartBundledBrowser -v ./internal/publish/
//
// 它守的是一件很容易悄悄坏掉的事：内置浏览器能不能被驱动起来。
// 「文件在」和「能启动」之间隔着签名、Frameworks 完整性、启动参数三道坎，
// 用户点上「扫码登录」才发现是最糟的时机。
func TestStartBundledBrowser(t *testing.T) {
	exe := os.Getenv("MW_EXE")
	ver := ""
	if exe == "" {
		exe, ver = bundledIn([]string{"../../"})
	}
	if exe == "" {
		t.Skip("仓库里没有内置浏览器，先跑 scripts/fetch-browser.sh")
	}
	t.Logf("用 %s（版本 %s）", exe, ver)

	// MW_SANDBOX=1 强制开着沙箱跑：用来区分「沙箱起不来」是环境的事
	// 还是我们参数的事。别的工具（Playwright）在 macOS 上也默认关沙箱，
	// 但不能因此就跟着关——先证明这里的沙箱确实起不来。
	forceSandbox := os.Getenv("MW_SANDBOX") == "1"
	noSandbox := os.Getenv("MW_NOSANDBOX") == "1" || (!forceSandbox && needsNoSandbox())

	var out bytes.Buffer
	opts := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
	opts = append(opts,
		chromedp.ExecPath(exe),
		chromedp.Flag("user-data-dir", t.TempDir()),
		chromedp.Flag("headless", true),
		chromedp.Flag("no-first-run", true),
		chromedp.Flag("no-default-browser-check", true),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.CombinedOutput(&out),
	)
	if noSandbox {
		opts = append(opts, chromedp.Flag("no-sandbox", true))
	}
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	defer allocCancel()
	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()
	ctx, cancel = context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	var ua string
	err := chromedp.Run(ctx,
		chromedp.Navigate("about:blank"),
		chromedp.Evaluate(`navigator.userAgent`, &ua),
	)
	log := out.String()
	if err != nil {
		t.Fatalf("内置浏览器起不来：%v\n--- 浏览器输出 ---\n%s", err, tail(log, 40))
	}
	if !strings.Contains(ua, "Chrome/") {
		t.Fatalf("UA 不像 Chrome：%q", ua)
	}
	t.Logf("UA = %s", ua)
	if strings.Contains(log, "sandbox initialization failed") {
		t.Logf("警告：沙箱初始化失败，但没有影响运行\n%s", tail(log, 10))
	}

	if err := chromedp.Run(ctx,
		chromedp.Navigate("https://www.xiaohongshu.com/explore"),
		chromedp.Sleep(2*time.Second),
	); err != nil {
		t.Fatalf("打不开小红书：%v", err)
	}
}

func tail(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
