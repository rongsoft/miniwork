//go:build windows

package publish

import (
	"os"

	"golang.org/x/sys/windows"
)

const (
	// SYNCHRONIZE 是只需要「能不能等这个进程退出」时用的权限，
	// 比 PROCESS_QUERY_INFORMATION 容易拿到——后者在对端进程属于
	// 另一个用户或更高完整性级别时会被拒。
	synchronizeAccess = 0x00100000
	// waitTimeout 是 WaitForSingleObject 的超时返回码：
	// 句柄还有效但对象没信号，也就是「进程还活着」。
	waitTimeout = 0x00000102
)

// processAlive 探活一个 pid。
//
// 早先这里直接 return true，注释写的是「Go 在 Windows 上没有 signal(0)
// 的等价物」。这句话不准确：OpenProcess + WaitForSingleObject(0) 就是
// 等价物，而且不需要 cgo。
//
// 为什么必须真的探一次：clearStaleLock 拿到的 pid 来自上次遗留的锁，
// 那个进程早就退出了，而 pid 会被系统回收给别的程序。无条件当它活着，
// 轻则每次启动白等 3 秒等一个永远不会消失的锁，重则 terminate 掉一个
// 恰好复用了这个 pid 的无关进程。
func processAlive(p *os.Process) bool {
	if p == nil || !sanePID(p.Pid) {
		return false
	}
	h, err := windows.OpenProcess(synchronizeAccess, false, uint32(p.Pid))
	if err != nil {
		// 打不开通常就是「进程不在了」，少数情况是权限不足。
		// 两种都按「不活着」处理：宁可不接管、走清锁重开那条路，
		// 也不要去动一个我们看不清的进程。
		return false
	}
	defer windows.CloseHandle(h)
	event, err := windows.WaitForSingleObject(h, 0)
	return err == nil && event == waitTimeout
}

// terminate 在 Windows 上直接结束进程。
//
// Windows 的 Chrome 用命名互斥体而不是 SingletonLock 文件来防重复启动，
// 走到这里说明 profile 确实被占着；Go 在 Windows 上只支持 Kill。
// pid 0 在 Windows 上表示「当前进程」，不能杀——sanePID 已经连 1 一起挡了。
func terminate(p *os.Process) {
	if p == nil || !sanePID(p.Pid) {
		return
	}
	_ = p.Kill()
}
