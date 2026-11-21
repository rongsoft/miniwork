package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Config 是服务端配置，对应 config.json。
//
// 字段刻意少：本地开发服务端不该有配置中心那套东西。
// token 为空表示不鉴权（方便 curl 调试），部署到公网前必须填上。
type Config struct {
	Port           int    `json:"port"`
	Token          string `json:"token"`
	RefreshMinutes int    `json:"refreshMinutes"` // 每隔多少分钟露出一批新内容
	BaseItems      int    `json:"baseItems"`      // 每个赛道初始露出条数
	StepItems      int    `json:"stepItems"`      // 每轮新露出的条数
	QuotaDay       int    `json:"quotaDay"`       // 每日请求配额，0 表示不限
	StaleAfterMin  int    `json:"staleAfterMin"`  // 快照超过多少分钟算 stale
}

func (c Config) RefreshInterval() time.Duration {
	m := c.RefreshMinutes
	if m <= 0 {
		m = 15
	}
	return time.Duration(m) * time.Minute
}

func defaultConfig() Config {
	return Config{
		Port:           18080,
		Token:          "miniwork-local-token",
		RefreshMinutes: 15,
		BaseItems:      12,
		StepItems:      3,
		QuotaDay:       0,
		StaleAfterMin:  120,
	}
}

func loadConfig(path string) (Config, error) {
	cfg := defaultConfig()
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		// 没有配置文件就写一份默认的，方便第一次跑起来改。
		if werr := writeConfig(path, cfg); werr != nil {
			return cfg, fmt.Errorf("写默认配置失败: %w", werr)
		}
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return cfg, fmt.Errorf("解析配置 %s 失败: %w", path, err)
	}
	if cfg.Port <= 0 || cfg.Port > 65535 {
		return cfg, fmt.Errorf("端口不合法: %d", cfg.Port)
	}
	return cfg, nil
}

func writeConfig(path string, cfg Config) error {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(path, b, 0o644)
}
