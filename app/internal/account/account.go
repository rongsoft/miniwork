// Package account 只做一件事：给这台机器一个稳定的身份。
//
// 为什么要机器码而不是让用户注册账号：客户是买断的个人用户，
// 打开就要能用。让他先填邮箱、收验证码、再登录，第一屏就流失了。
// 机器码在后台完成「注册」，用户看到的只是一个可以改的名字和手机号。
//
// 不做的事：不上传、不联网、不做指纹追踪。机器码在本机算出来，
// 只当主键用；哪天接了服务端，也是拿它去换 token，而不是反过来。
package account

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Code 返回本机机器码，形如 4A7F-19C2-8E30-5B61。
//
// 结果只算一次：Windows 上取主板序列要走 PowerShell，几百毫秒，
// 每次进「账号」页都算一遍会让人以为页面卡了。
var (
	once sync.Once
	code string
)

func Code() string {
	once.Do(func() { code = format(hash(rawMachineID())) })
	return code
}

// hash 把原始硬件标识压成 16 位十六进制。
//
// 不直接展示原始值：主板 UUID 和序列号在部分机型上能反查到具体设备，
// 对一个只用来当主键的东西，没必要暴露那么细。
func hash(raw string) string {
	sum := sha256.Sum256([]byte("miniwork/" + raw))
	return strings.ToUpper(hex.EncodeToString(sum[:8]))
}

func format(h string) string {
	if len(h) != 16 {
		return h
	}
	return h[0:4] + "-" + h[4:8] + "-" + h[8:12] + "-" + h[12:16]
}

// rawMachineID 依次尝试：系统硬件标识 → 本地生成的设备文件。
//
// 兜底是必须的：虚拟机、精简版系统、容器里都取不到硬件码，
// 那种情况下如果直接返回空，整台机器就没有账号，比多一个文件更糟。
func rawMachineID() string {
	if id := platformID(); usable(id) {
		return id
	}
	return persistedID()
}

func usable(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" {
		return false
	}
	// 全 0、全 F 是虚拟机/工控板常见的「有值但无意义」。
	compact := strings.NewReplacer("-", "", " ", "", "0", "").Replace(id)
	if compact == "" {
		return false
	}
	return !strings.EqualFold(compact, strings.Repeat("F", len(compact)))
}

func platformID() string {
	switch runtime.GOOS {
	case "darwin":
		return ioregUUID()
	case "windows":
		return windowsUUID()
	default:
		if b, err := os.ReadFile("/etc/machine-id"); err == nil {
			return string(b)
		}
		if b, err := os.ReadFile("/var/lib/dbus/machine-id"); err == nil {
			return string(b)
		}
		return ""
	}
}

var ioregRe = regexp.MustCompile(`"IOPlatformUUID"\s*=\s*"([^"]+)"`)

func ioregUUID() string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ioreg", "-rd1", "-c", "IOPlatformExpertDevice").Output()
	if err != nil {
		return ""
	}
	if m := ioregRe.FindSubmatch(out); len(m) == 2 {
		return string(m[1])
	}
	return ""
}

func windowsUUID() string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// 用 CIM 而不是 wmic：wmic 在 Windows 11 24H2 起默认不装了。
	out, err := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive",
		"-Command", "(Get-CimInstance Win32_ComputerSystemProduct).UUID").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// persistedID 在配置目录里放一个随机串，当作「没有硬件码时的机器码」。
//
// 换机会变、重装会变，这是这个兜底的已知代价。写进注释而不是藏起来：
// 将来做授权时，得靠服务端发现「同一个用户报了两个设备」来兜。
func persistedID() string {
	path := filepath.Join(configDir(), "device.id")
	if b, err := os.ReadFile(path); err == nil {
		if s := strings.TrimSpace(string(b)); s != "" {
			return s
		}
	}
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return ""
	}
	id := hex.EncodeToString(buf[:])
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	_ = os.WriteFile(path, []byte(id), 0o600)
	return id
}

// configDir 取配置目录。和 config 包保持同一个位置，但这里不 import 它：
// account 是最底层的身份，不该反过来依赖配置加载（那会形成环）。
func configDir() string {
	if v := os.Getenv("MINIWORK_CONFIG_DIR"); v != "" {
		return v
	}
	base, err := os.UserConfigDir()
	if err != nil {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".miniwork")
	}
	return filepath.Join(base, "miniwork")
}
