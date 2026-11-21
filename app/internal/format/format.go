// Package format 统一 CLI 与界面的展示口径，避免两处各写一套导致数字对不上。
package format

import (
	"fmt"
	"time"
)

// Platform 把平台代码转成中文名。
//
// 认得出的一律在这里加，不要在界面或服务层另写一份 switch：
// 同一条内容在选题列表叫「小红书」、在台账里叫「xiaohongshu」，
// 用户会以为是两个地方。
func Platform(p string) string {
	switch p {
	case "xiaohongshu", "xhs":
		return "小红书"
	case "douyin":
		return "抖音"
	case "wechat", "gzh":
		return "公众号"
	}
	return p
}

// Heat 把互动量压缩成 1.2万 / 3.4k。
func Heat(n int64) string {
	switch {
	case n >= 10000:
		return fmt.Sprintf("%.1f万", float64(n)/10000)
	case n >= 1000:
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	case n == 0:
		return "—"
	}
	return fmt.Sprintf("%d", n)
}

// PlanDate 把 2026-11-15 缩成 11-15。日历格子和卡片角标都放不下年份，
// 而排期永远在「最近几周」，丢掉年份不会产生歧义。
func PlanDate(s string) string {
	if len(s) < 10 {
		return s
	}
	return s[5:10]
}

// Age 把发布时间转成「3 小时前」，界面和终端用同一套说法。
func Age(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	d := time.Since(t)
	switch {
	case d < 0:
		return "刚刚"
	case d < time.Hour:
		return "1 小时内"
	case d < 24*time.Hour:
		return fmt.Sprintf("%.0f 小时前", d.Hours())
	default:
		return fmt.Sprintf("%.0f 天前", d.Hours()/24)
	}
}
