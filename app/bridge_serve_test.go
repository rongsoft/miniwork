//go:build bridge

package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"rongsoft/miniwork/app/internal/source"
	"rongsoft/miniwork/app/internal/store"
)

// TestServeBridge 把 TopicService 暴露成一个 HTTP 接口，给 headless 前端当 bridge 用。
//
// 为什么不用假数据：假数据只能证明「前端读不存在的字段时不崩」，证明不了
// 「点了按钮，库里真的多了一行」。这里接的是真 store + 真 pipeline，
// 只有 AI 换成脚本化假模型（不花 API 额度、也不受网络影响）。
//
// 只在 -tags bridge 下编译，且要显式 -run 才跑，普通 go test ./... 完全不受影响。
// 跑法：
//
//	MW_DB=/tmp/mw-e2e.db MW_DIST=frontend/dist MW_RT=/tmp/mwrt/runtime.js \
//	  go test -tags bridge -run TestServeBridge -timeout 0 -count=1 .
func TestServeBridge(t *testing.T) {
	dbPath := mustEnv("MW_DB")
	dist := mustEnv("MW_DIST")
	rtPath := os.Getenv("MW_RT")
	addr := os.Getenv("MW_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8732"
	}

	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("打开库失败: %v", err)
	}
	defer st.Close()

	// 数据源用 mock：内置示例条目，不依赖外网服务。
	src, label := source.Build(source.Spec{Kind: "mock"})
	svc := NewTopicService(st, src, label, scriptedAI{}, dbPath)
	ver, _ := st.SchemaVersion()
	log.Printf("bridge 就绪 库=%s 源=%s 版本=%d", dbPath, label, ver)

	mux := http.NewServeMux()

	// 前端页面与静态资源。
	//
	// index.html 里引用的是 ./style.css、./app.js，也就是从站点根开始的路径，
	// 所以静态根必须是 dist 本身。另外要显式补 Content-Type：
	// FileServer 靠系统 mime 表猜扩展名，某些机器上那张表是空的，
	// 浏览器会拒收样式，截图里看到的是一片没样式的裸 HTML。
	fs := http.FileServer(http.Dir(dist))
	serveAsset := func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, ".css"):
			w.Header().Set("Content-Type", "text/css; charset=utf-8")
		case strings.HasSuffix(r.URL.Path, ".js"):
			w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		case strings.HasSuffix(r.URL.Path, ".html"):
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
		}
		fs.ServeHTTP(w, r)
	}
	mux.Handle("/dist/", http.StripPrefix("/dist", http.HandlerFunc(serveAsset)))
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.ServeFile(w, r, filepath.Join(dist, "index.html"))
			return
		}
		serveAsset(w, r)
	}))

	// 假的 Wails 运行时：不拦截请求，只是把 /call 转出去。
	if rtPath != "" {
		mux.HandleFunc("/wails/runtime.js", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
			http.ServeFile(w, r, rtPath)
		})
	}

	// 方法名 → 调用号。前端绑定里写死了 ByID(hash)，这里把 hash 反查回名字。
	mux.HandleFunc("/ids.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(methodIDs())
	})

	mux.HandleFunc("/call", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string            `json:"method"`
			Args   []json.RawMessage `json:"args"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, map[string]any{"ok": false, "error": "请求格式不对: " + err.Error()})
			return
		}
		data, errMsg := invoke(svc, req.Method, req.Args)
		if errMsg != "" {
			writeJSON(w, map[string]any{"ok": false, "error": errMsg})
			return
		}
		writeJSON(w, map[string]any{"ok": true, "data": data})
	})

	log.Printf("监听 %s", addr)
	// 只记 4xx/5xx：正常请求刷屏会把有用的几行冲掉。
	wrapped := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRec{w: w, code: 200}
		mux.ServeHTTP(rec, r)
		if rec.code >= 400 {
			log.Printf("!! %d %s", rec.code, r.URL.Path)
		}
	})
	if err := http.ListenAndServe(addr, wrapped); err != nil {
		t.Fatalf("服务退出: %v", err)
	}
}

type statusRec struct {
	w    http.ResponseWriter
	code int
}

func (s *statusRec) Header() http.Header         { return s.w.Header() }
func (s *statusRec) Write(b []byte) (int, error) { return s.w.Write(b) }
func (s *statusRec) WriteHeader(code int)        { s.code = code; s.w.WriteHeader(code) }

// invoke 用反射调方法：前端传什么参数就转成对应类型，
// 免得每加一个方法就要在这里补一行 switch。
func invoke(svc *TopicService, method string, args []json.RawMessage) (any, string) {
	if method == "" {
		return nil, "方法名为空"
	}
	m := reflect.ValueOf(svc).MethodByName(method)
	if !m.IsValid() {
		return nil, "没有这个方法: " + method
	}
	mt := m.Type()

	in := make([]reflect.Value, mt.NumIn())
	for i := range in {
		pt := mt.In(i)
		holder := reflect.New(pt)
		if i < len(args) && len(args[i]) > 0 && string(args[i]) != "null" {
			if err := json.Unmarshal(args[i], holder.Interface()); err != nil {
				return nil, fmt.Sprintf("参数 %d 转不成 %s: %v", i, pt.String(), err)
			}
		}
		in[i] = holder.Elem()
	}

	out := m.Call(in)
	// 约定最后一个返回值是 error（Go 侧就是这样写的）
	if n := len(out); n > 0 {
		if e, ok := out[n-1].Interface().(error); ok {
			if e != nil {
				return nil, e.Error()
			}
			out = out[:n-1]
		}
	}
	if len(out) == 0 {
		return nil, ""
	}
	return out[0].Interface(), ""
}

// methodIDs 从绑定文件里反查 hash → 方法名。
// 硬编码一份映射也能跑，但改了方法名就会静默失效；现查现用不会走偏。
func methodIDs() map[string]string {
	out := map[string]string{}
	root := filepath.Join(mustEnv("MW_DIST"), "bindings")
	filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(p, ".js") {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		// export function Name(...) {\n    return $Call.ByID(123...)
		for _, line := range strings.Split(string(b), "\n") {
			i := strings.Index(line, "$Call.ByID(")
			if i < 0 {
				continue
			}
			rest := line[i+len("$Call.ByID("):]
			j := strings.IndexByte(rest, ',')
			if j < 0 {
				j = strings.IndexByte(rest, ')')
			}
			if j <= 0 {
				continue
			}
			id := strings.TrimSpace(rest[:j])
			// 往回找最近的 export function
			name := lastExported(string(b), strings.Index(string(b), line))
			if name != "" {
				out[id] = name
			}
		}
		return nil
	})
	return out
}

func lastExported(src string, pos int) string {
	head := src[:pos]
	i := strings.LastIndex(head, "export function ")
	if i < 0 {
		return ""
	}
	rest := head[i+len("export function "):]
	j := strings.IndexByte(rest, '(')
	if j < 0 {
		return ""
	}
	return strings.TrimSpace(rest[:j])
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(v)
}

func mustEnv(k string) string {
	v := os.Getenv(k)
	if v == "" {
		panic("缺少环境变量 " + k)
	}
	return v
}
