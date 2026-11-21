package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadWritesDefaultWhenMissing(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	c, err := LoadFrom(p)
	if err != nil {
		t.Fatalf("缺文件不该报错: %v", err)
	}
	if c.Source != "mock" {
		t.Fatalf("默认应为 mock，实际 %q", c.Source)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("应把默认配置写进磁盘，方便用户改: %v", err)
	}
}

func TestLoadNormalizesAndRoundTrips(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	in := Config{Source: "REMOTE", Remote: Remote{BaseURL: "https://api.example.com/", Token: " k "}}
	if err := in.SaveTo(p); err != nil {
		t.Fatal(err)
	}
	out, err := LoadFrom(p)
	if err != nil {
		t.Fatal(err)
	}
	if out.Source != "remote" {
		t.Fatalf("source 应转小写，实际 %q", out.Source)
	}
	if out.Remote.BaseURL != "https://api.example.com" {
		t.Fatalf("地址尾部斜杠应去掉，实际 %q", out.Remote.BaseURL)
	}
	if out.Remote.Token != "k" {
		t.Fatalf("token 应去空白，实际 %q", out.Remote.Token)
	}
}

func TestLoadTreatsUnknownSourceAsMock(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, []byte(`{"source":"yizhuan"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := LoadFrom(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.Source != "mock" {
		t.Fatalf("未知数据源应收敛为 mock，实际 %q", c.Source)
	}
}

func TestBrokenJSONFallsBackToDefault(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, []byte(`{source: mock,}`), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := LoadFrom(p)
	if err == nil {
		t.Fatal("坏配置应报错，让调用方打日志")
	}
	if c.Source != "mock" {
		t.Fatalf("报错时也应返回可用的默认值，实际 %q", c.Source)
	}
}

func TestEnvOverridesFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, []byte(`{"source":"mock"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MINIWORK_SOURCE", "remote")
	t.Setenv("MINIWORK_API_BASE", "http://127.0.0.1:8080/")
	t.Setenv("MINIWORK_API_TOKEN", "tk")

	c, err := LoadFrom(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.Source != "remote" || c.Remote.BaseURL != "http://127.0.0.1:8080" || c.Remote.Token != "tk" {
		t.Fatalf("环境变量应覆盖文件，实际 %+v", c)
	}
}

// 回归：文件不存在时曾提前 return，把环境变量整个跳过了。
// 装机脚本和 CI 只给环境变量、不落文件，这个洞会让 remote 永远不生效。
func TestEnvOverridesWhenFileMissing(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("MINIWORK_SOURCE", "remote")
	t.Setenv("MINIWORK_API_BASE", "http://127.0.0.1:8080")

	c, err := LoadFrom(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.Source != "remote" || c.Remote.BaseURL != "http://127.0.0.1:8080" {
		t.Fatalf("没有配置文件时也要认环境变量，实际 %+v", c)
	}
}
