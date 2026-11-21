package publish

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
)

// 一个 profile 目录同时只能被一个 Chrome 进程使用。
//
// 这个约束平时不会碰到，但它会在最糟的时刻冒出来：用户点过「扫码登录」，
// 窗口开着停在扫码页；随后应用重启（或者我们回收了会话）——引擎手里
// 已经没有那个会话了，可进程还在占着 profile。这时再起新进程，Chrome
// 检测到锁已存在就地 abort，报的是一句
//
//	Failed to create .../SingletonLock: File exists (17)
//
// 用户看到的是「浏览器起不来」，完全想不到是上次那个窗口没关。
//
// 处理顺序：能接管就接管（那个窗口可能正停在扫码页，杀了最烦人），
// 接管不了才清锁重开。

// adoptExisting 尝试接管已经开着的浏览器进程。
//
// 返回的 cancel 只管断开连接，不会关掉那个窗口——窗口是用户的，
// 关不关由他自己决定。
//
// 这里**不能**用带超时的子 context 跑一次 chromedp.Run 来「验证能不能用」：
// chromedp 第一次 Run 会把连上的页面目标挂到传进来的那个 ctx 上，
// 子 ctx 一到点就取消，之后所有命令只回一句 context canceled。
// 而这句话会冒在「设置视口」那一步，看着像浏览器坏了，其实是自己取消的。
// 所以验证改成纯 HTTP：问一下调试端口上还挂着页面没有。
func adoptExisting(dir string) (ctx context.Context, cancel func(), ok bool) {
	port, ws := cdpEndpoint(dir)
	if ws == "" || !hasPageTarget(port) {
		return nil, nil, false
	}
	allocCtx, allocCancel := chromedp.NewRemoteAllocator(context.Background(), ws)
	ctx, ctxCancel := chromedp.NewContext(allocCtx)
	cancel = func() { ctxCancel(); allocCancel() }
	return ctx, cancel, true
}

// hasPageTarget 看端口上还挂着页面。
//
// 只看端口通不通不够：进程可能刚退出、端口可能被别的程序占着，
// 也可能只剩一个浏览器目标、没有任何页面——那种状态连上去什么也干不了。
func hasPageTarget(port int) bool {
	if port <= 0 {
		return false
	}
	cli := &http.Client{Timeout: 1500 * time.Millisecond}
	resp, err := cli.Get("http://127.0.0.1:" + strconv.Itoa(port) + "/json/list")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	var list []struct {
		Type string `json:"type"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return false
	}
	for _, t := range list {
		if t.Type == "page" {
			return true
		}
	}
	return false
}

// cdpEndpoint 从 profile 目录里读出遗留进程的调试端口和 ws 地址。
func cdpEndpoint(dir string) (port int, ws string) {
	raw, err := os.ReadFile(filepath.Join(dir, "DevToolsActivePort"))
	if err != nil {
		return 0, ""
	}
	// 文件第一行是端口，第二行是 /devtools/browser/<uuid>，末尾可能没换行。
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) == 0 {
		return 0, ""
	}
	n, err := strconv.Atoi(strings.TrimSpace(lines[0]))
	if err != nil || n <= 0 {
		return 0, ""
	}
	cli := &http.Client{Timeout: 1500 * time.Millisecond}
	resp, err := cli.Get("http://127.0.0.1:" + strconv.Itoa(n) + "/json/version")
	if err != nil {
		return 0, ""
	}
	defer resp.Body.Close()
	var v struct {
		WS string `json:"webSocketDebuggerUrl"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return 0, ""
	}
	return n, v.WS
}

// cdpWSURL 只取地址，给测试用。
//
// Chrome 启动时会把实际端口写进 DevToolsActivePort（我们用的是
// --remote-debugging-port=0，端口由系统分配）。再问一次 /json/version，
// 拿它自己报的 ws 地址——比自己拼字符串可靠，也顺便确认对面真是 Chrome。
func cdpWSURL(dir string) string {
	_, ws := cdpEndpoint(dir)
	return ws
}

// clearStaleLock 清掉上次进程留下的锁。
//
// 只在上面的接管失败后调用：这时要么进程已经死了、锁是残留，
// 要么端口不通没法用——两种都得让位给新进程。
func clearStaleLock(dir string) {
	if pid := lockPID(dir); pid > 0 {
		if p, err := os.FindProcess(pid); err == nil && processAlive(p) {
			terminate(p)
			// 锁是进程退出时删的。立刻起新进程还会撞上它，
			// 所以等锁真的消失（最多 3 秒）。等不到就硬删文件。
			lock := filepath.Join(dir, "SingletonLock")
			for i := 0; i < 30; i++ {
				if _, err := os.Lstat(lock); err != nil {
					break
				}
				time.Sleep(100 * time.Millisecond)
			}
		}
	}
	for _, n := range []string{"SingletonLock", "SingletonSocket", "SingletonCookie"} {
		if err := os.Remove(filepath.Join(dir, n)); err != nil && !os.IsNotExist(err) {
			// 删不掉不致命：Chrome 若还认为锁在，会再报一次，
			// 那时错误信息里带 pid，用户至少知道该怎么办。
			_ = err
		}
	}
}

// startError 把 Chrome 的启动报错翻译成用户能动手处理的一句话。
//
// 原始报错长这样：
//
//	chrome failed to start: [65694:...:ERROR:process_singleton_posix.cc:349]
//	Failed to create .../SingletonLock: File exists (17)
//
// 原样丢给用户，他只能来问我们。上一版还自己猜了一句「浏览器版本太旧」，
// 结果把唯一有用的信息（锁被占）盖掉了——猜错方向比不说更糟。
func startError(err error) error {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "SingletonLock"), strings.Contains(msg, "ProcessSingleton"):
		return fmt.Errorf("这个平台的浏览器窗口还被另一个进程占着（通常是上次开的窗口没关）。" +
			"把那个窗口关掉再点一次；找不到窗口就直接重启本程序")
	case strings.Contains(msg, "DevToolsActivePort"), strings.Contains(msg, "context canceled"):
		return fmt.Errorf("浏览器起来了但没连上，通常是安全软件拦了调试端口。再点一次；一直不行把日志发我: %w", err)
	}
	return fmt.Errorf("浏览器启动失败: %w", err)
}

// lockPID 从 SingletonLock 里读出占用进程的 pid。
// 它是符号链接，指向 "<主机名>-<pid>"。
func lockPID(dir string) int {
	target, err := os.Readlink(filepath.Join(dir, "SingletonLock"))
	if err != nil {
		return 0
	}
	i := strings.LastIndex(target, "-")
	if i < 0 {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(target[i+1:]))
	if err != nil || !sanePID(pid) {
		return 0
	}
	return pid
}

// sanePID 判断一个 pid 值能不能拿去探活、能不能拿去发信号。
//
// 这不是防御式编程的洁癖，是必须的：unix 上 kill(-1, sig) 的含义是
// 「给当前用户能发信号的**所有**进程发这个信号」，kill(0, sig) 是
// 「给自己所在的进程组」。而 pid 一旦超出 int32 范围（比如锁文件里
// 被写成 4294967295），转成 int32 正好是 -1 —— 一次「清理残留锁」
// 就会把用户桌面上开着的所有程序全部 SIGTERM 掉。
//
// 这个坑真踩过：测试里图省事用 4294967295 当「不存在的进程」，
// 结果测试一跑就把编辑器一起杀了。所以校验必须放在唯一入口，
// 而不是指望每个调用方自己记得。
//
// 上限 1<<22：Linux 默认 pid_max 上限是 4194304，macOS 远小于此，
// 留足余量又不至于放过明显的脏值。下限 2：pid 1 是 init / launchd，
// 动它等于毁系统。
func sanePID(pid int) bool {
	return pid >= 2 && pid <= 1<<22
}
