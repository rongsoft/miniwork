//go:build !windows

package publish

import (
	"os"
	"syscall"
	"time"
)

// processAlive 探活：Signal(0) 只做权限/存在性检查，不发真信号。
//
// 先过 sanePID：kill(-1, 0) 在所有 unix 上都算「成功」（只要有进程能发），
// 于是无效 pid 会被判成「活着」，接着 terminate 就真的去 kill(-1, SIGTERM)。
func processAlive(p *os.Process) bool {
	if p == nil || !sanePID(p.Pid) {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}

// terminate 让遗留的浏览器进程退出。
//
// 先 SIGTERM 而不是直接 SIGKILL：Chrome 收到 SIGTERM 会正常收尾，
// 把 cookie 和登录态刷盘。硬杀有可能丢掉刚刚写入的登录态，
// 用户就得再扫一次码——这正是我们要避免的事。
func terminate(p *os.Process) {
	if p == nil || !sanePID(p.Pid) {
		return
	}
	if err := p.Signal(syscall.SIGTERM); err != nil {
		_ = p.Kill()
		return
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.FindProcess(p.Pid); err != nil {
			return
		}
		// Signal(0) 只探活，不真发信号。
		if err := p.Signal(syscall.Signal(0)); err != nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	_ = p.Kill()
}
