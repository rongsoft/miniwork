package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 绑定文件里出现裸模块名就等于整个界面不工作。
//
// 踩过两次：一次是手工执行 `wails3 generate bindings` 时忘了 `-b`，
// 生成出来的每个文件都写着 `from "@wailsio/runtime"`。浏览器不认裸模块名，
// 模块图在解析阶段就断了 —— HTML 和 CSS 照常加载，所以窗口看着「没问题」，
// 只是所有数字停在 0、列表空着、按钮点了没反应，最容易误判成后端挂了。
//
// 这条测试守着「生成命令必须带 -b」这个约定：改错参数，`go test` 立刻红。
func TestBindingsUseBundledRuntime(t *testing.T) {
	root := filepath.Join("frontend", "dist", "bindings")
	scanned := 0
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".js") {
			return nil
		}
		scanned++
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(data), `from "@wailsio/runtime"`) ||
			strings.Contains(string(data), `from '@wailsio/runtime'`) {
			t.Errorf("%s 引用的是 npm 包名 @wailsio/runtime；\n"+
				"请用 `wails3 generate bindings -b -d frontend/dist/bindings` 重新生成（-b 不能省）", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("遍历绑定目录失败: %v", err)
	}
	if scanned == 0 {
		t.Fatalf("%s 下没找到任何 .js 绑定文件，路径可能变了", root)
	}
}
