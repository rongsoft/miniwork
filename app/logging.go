package main

import (
	"io"
	"log"
	"os"
	"path/filepath"
)

// maxLogBytes 是单个日志文件的上限，超了就轮转一次。
//
// 只保留当前 + 上一份：这个日志是给「出问题时看一眼」用的，不是审计。
// 无限追加大半年后变成几百兆，用户删不掉、我们也不会去翻。
const maxLogBytes = 2 << 20

// setupLog 把日志接到文件上，同时保留控制台输出。
//
// 为什么必须要文件：打包用 -H windowsgui，进程没有控制台，log.Printf
// 写到一个无效句柄上等于直接扔掉。用户晚上在 Windows 上跑，一旦「双击没反应」
// 或者「托盘没出来」，除了这个文件没有任何线索。macOS 从 Finder 双击启动时
// stderr 同样没人接，所以两个平台都落盘。
//
// 返回的函数负责关文件。写在文件之后的控制台输出是"尽力而为"：
// Windows 上那一路会返回 "handle is invalid"，MultiWriter 会把它当错误往上报，
// 但文件那一路已经写完了——顺序不能反，反了就是文件也写不进去。
func setupLog(dir string) func() {
	if dir == "" {
		dir = "."
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("创建日志目录失败，本次只输出到控制台: %v", err)
		return func() {}
	}

	path := filepath.Join(dir, "miniwork.log")
	if fi, err := os.Stat(path); err == nil && fi.Size() > maxLogBytes {
		// 轮转失败不算事：大不了这个文件继续变大，功能不受影响。
		_ = os.Remove(path + ".1")
		_ = os.Rename(path, path+".1")
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		log.Printf("打开日志文件失败，本次只输出到控制台: %v", err)
		return func() {}
	}

	log.SetOutput(io.MultiWriter(f, os.Stderr))
	// 带毫秒：定时刷新、托盘刷新这类问题要看「隔了多久」，
	// 秒级时间戳分不清「一秒内跑完」和「卡了半分钟」。
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.Printf("──── 启动 %s ────", path)
	return func() { _ = f.Close() }
}
