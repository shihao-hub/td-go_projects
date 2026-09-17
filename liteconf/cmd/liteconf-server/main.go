// liteconf-server：轻量配置中心服务端。
// 单实例 HTTP 服务，托管 configs/{app}/{env}.json 配置文件，
// 提供读取/写入/发现/长轮询 API 与外部编辑周期检测。
// 定位内网/本机可信环境：HTTP 明文、无鉴权。
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/shihao-hub/liteconf/internal/server"
)

// dataBase 返回本工具数据基准目录（遵循仓库数据目录约定）：
// %APPDATA%\language_projects\liteconf，取不到 APPDATA 回退 ~/.language_projects/liteconf
func dataBase() (string, error) {
	if dir, err := os.UserConfigDir(); err == nil && dir != "" {
		return filepath.Join(dir, "language_projects", "liteconf"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve data dir: %w", err)
	}
	return filepath.Join(home, ".language_projects", "liteconf"), nil
}

// setupLog 初始化 slog：同时写 stderr 与 logs/liteconf-server.log（追加）
func setupLog(base string) (*os.File, error) {
	logDir := filepath.Join(base, "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return nil, fmt.Errorf("create log dir: %w", err)
	}
	logFile, err := os.OpenFile(filepath.Join(logDir, "liteconf-server.log"),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open log file: %w", err)
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(io.MultiWriter(os.Stderr, logFile), nil)))
	return logFile, nil
}

func main() {
	base, err := dataBase()
	if err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
	defaultRoot := filepath.Join(base, "configs")

	addr := flag.String("addr", ":8646", "HTTP 监听地址")
	root := flag.String("root", defaultRoot, "配置根目录（configs/{app}/{env}.json）")
	poll := flag.Duration("poll", 5*time.Second, "外部编辑检测间隔")
	flag.Parse()

	logFile, err := setupLog(base)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
	defer logFile.Close()

	slog.Warn("liteconf-server has NO authentication and serves plain HTTP; restrict to trusted LAN/localhost")
	slog.Info("starting liteconf-server", "addr", *addr, "root", *root, "poll", *poll)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	bc := server.NewBroadcaster()
	st, err := server.NewStore(*root, bc.Notify)
	if err != nil {
		slog.Error("init store failed", "err", err)
		os.Exit(1)
	}
	slog.Info("configs loaded", "count", len(st.List()))

	poller := server.NewPoller(st, *poll)
	go poller.Run(ctx)

	srv := &http.Server{
		Addr:    *addr,
		Handler: server.NewMux(st, bc),
	}
	go func() {
		slog.Info("http server listening", "addr", *addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("http server failed", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("graceful shutdown failed", "err", err)
	}
	slog.Info("bye")
}
