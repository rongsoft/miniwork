package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

func main() {
	configPath := flag.String("config", "config.json", "配置文件路径")
	flag.Parse()

	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	st := newStore(cfg)

	mux := http.NewServeMux()
	mux.HandleFunc("/data/hotspots", st.hotspotsHandler)
	mux.HandleFunc("/health", st.healthHandler)
	mux.HandleFunc("/_refresh", st.refreshHandler)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			writeJSON(w, http.StatusOK, map[string]any{
				"service": "miniwork data server",
				"endpoints": []string{
					"GET /data/hotspots?niche=职场&limit=60",
					"GET /health",
					"POST /_refresh",
				},
			})
			return
		}
		http.NotFound(w, r)
	})

	srv := &http.Server{
		Addr:              ":" + itoa(cfg.Port),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// 定时重算快照：让随时间放量生效，刷新的页面能看到新内容。
	ticker := time.NewTicker(cfg.RefreshInterval())
	defer ticker.Stop()
	go func() {
		for range ticker.C {
			st.refresh()
			log.Printf("已刷新快照: %s", st.current().GeneratedAt)
		}
	}()

	// 优雅关闭：部署到服务器上用 systemctl stop 不会打断在途请求。
	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-done
		log.Println("收到退出信号，正在关闭...")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	exe, _ := os.Executable()
	log.Printf("数据服务启动 :%d  config=%s  exe=%s", cfg.Port, abs(*configPath), exe)
	log.Printf("token 鉴权: %v  配额: %d/天  刷新间隔: %v", cfg.Token != "", cfg.QuotaDay, cfg.RefreshInterval())
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("服务退出: %v", err)
	}
	log.Println("已停止")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func abs(p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	d, err := os.Getwd()
	if err != nil {
		return p
	}
	return filepath.Join(d, p)
}
