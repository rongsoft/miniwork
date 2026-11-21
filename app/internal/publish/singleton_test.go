package publish

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// 锁文件是符号链接，指向 "<主机名>-<pid>"。
func TestLockPIDParses(t *testing.T) {
	dir := t.TempDir()
	lock := filepath.Join(dir, "SingletonLock")
	if err := os.Symlink("MacdeMacBook-Air.local-58798", lock); err != nil {
		t.Skipf("这个文件系统不让建符号链接: %v", err)
	}
	if got := lockPID(dir); got != 58798 {
		t.Fatalf("pid = %d，应该是 58798", got)
	}
}

// 没有锁是正常状态（第一次启动），不能返回垃圾 pid 去杀进程。
func TestLockPIDMissing(t *testing.T) {
	if got := lockPID(t.TempDir()); got != 0 {
		t.Fatalf("没锁时 pid 应该是 0，实际 %d", got)
	}
}

// 锁指向一个已退出的进程：三个锁文件都要清掉，否则新进程起不来。
func TestClearStaleLockRemovesFiles(t *testing.T) {
	dir := t.TempDir()
	// 必须用「真实存在过、已经退出」的 pid。
	// 上一版图省事写了 4294967295，转 int32 是 -1，kill(-1, SIGTERM)
	// 会把当前用户所有进程全杀掉——测试一跑就把编辑器干掉了。
	dead := deadPID(t)
	if err := os.Symlink("host-"+strconv.Itoa(dead), filepath.Join(dir, "SingletonLock")); err != nil {
		t.Skipf("这个文件系统不让建符号链接: %v", err)
	}
	for _, n := range []string{"SingletonSocket", "SingletonCookie"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	start := time.Now()
	clearStaleLock(dir)
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("清残留锁不该等这么久（%v）——进程已经不在，等锁是白等", d)
	}
	for _, n := range []string{"SingletonLock", "SingletonSocket", "SingletonCookie"} {
		if _, err := os.Lstat(filepath.Join(dir, n)); err == nil {
			t.Fatalf("%s 还在", n)
		}
	}
}

// 脏 pid 必须在解析这一层就被挡掉，绝不能流到 kill 那里去。
//
// 这几条值各自对应一种误伤：4294967295 转 int32 是 -1（杀光当前用户所有进程）、
// -1 是字面的广播、0 是当前进程组、1 是 launchd/init。
func TestLockPIDRejectsUnsafe(t *testing.T) {
	for _, bad := range []string{"4294967295", "-1", "0", "1", "2147483648", "abc", ""} {
		dir := t.TempDir()
		if err := os.Symlink("host-"+bad, filepath.Join(dir, "SingletonLock")); err != nil {
			t.Skipf("这个文件系统不让建符号链接: %v", err)
		}
		if got := lockPID(dir); got != 0 {
			t.Fatalf("pid %q 应该被判为无效（返回 0），实际 %d", bad, got)
		}
	}
}

// 回归测试：即使有人绕过 lockPID 直接塞一个 -1 进来，
// terminate 和 processAlive 也必须自己守住。
func TestTerminateRefusesUnsafePID(t *testing.T) {
	for _, pid := range []int{-1, 0, 1, 4294967295} {
		p := &os.Process{Pid: pid}
		if processAlive(p) {
			t.Fatalf("pid %d 不该被判成活着", pid)
		}
		// 这里不许有任何副作用——真发了信号，跑测试的编辑器就没了。
		terminate(p)
	}
}

// deadPID 起一个立刻退出的进程，拿它已经作废的 pid。
func deadPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("/usr/bin/true")
	if err := cmd.Start(); err != nil {
		t.Skipf("起不了子进程: %v", err)
	}
	pid := cmd.Process.Pid
	_ = cmd.Wait()
	return pid
}

// DevToolsActivePort + /json/version 是接管遗留进程的唯一入口。
// 这条走的是真实 HTTP，只是把对面换成了假服务端。
func TestCDPWSURLFromDevToolsActivePort(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/json/version" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{
			"webSocketDebuggerUrl": "ws://127.0.0.1:1/devtools/browser/fake",
		})
	}))
	defer srv.Close()
	port := srv.Listener.Addr().(*net.TCPAddr).Port

	dir := t.TempDir()
	// 真实文件末尾没有换行，第一行端口、第二行路径。
	content := strconv.Itoa(port) + "\n/devtools/browser/abc"
	if err := os.WriteFile(filepath.Join(dir, "DevToolsActivePort"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	got := cdpWSURL(dir)
	if !strings.HasPrefix(got, "ws://") || !strings.HasSuffix(got, "/devtools/browser/fake") {
		t.Fatalf("ws 地址 = %q", got)
	}
}

// 目录里没有 DevToolsActivePort（进程从没起过，或已正常退出）→ 不接管。
func TestCDPWSURLMissing(t *testing.T) {
	if got := cdpWSURL(t.TempDir()); got != "" {
		t.Fatalf("不该拿到地址，实际 %q", got)
	}
}

// 端口通但不是 Chrome（比如被别的程序占了）→ 不能当成可接管的浏览器。
func TestCDPWSURLRejectsNonChrome(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html>not chrome</html>"))
	}))
	defer srv.Close()
	port := srv.Listener.Addr().(*net.TCPAddr).Port

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "DevToolsActivePort"),
		[]byte(strconv.Itoa(port)+"\n/devtools/browser/abc"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := cdpWSURL(dir); got != "" {
		t.Fatalf("对面的响应不是 CDP，却拿到 %q", got)
	}
}

// 报错必须说人话，而且不能再自己猜「版本太旧」把真正的原因盖掉。
func TestStartErrorTranslatesSingleton(t *testing.T) {
	err := startError(errString("chrome failed to start: Failed to create SingletonLock: File exists (17)"))
	if !strings.Contains(err.Error(), "窗口") {
		t.Fatalf("没把「锁被占」翻成窗口的事：%v", err)
	}
	if strings.Contains(err.Error(), "版本太旧") {
		t.Fatalf("不该再猜版本太旧：%v", err)
	}
}

type errString string

func (e errString) Error() string { return string(e) }
