// aiquickd 是 aiquick 的后端守护进程：
// 无参数启动后进入行式 JSON 协议模式（stdin 读请求，stdout 写应答/事件），
// 也可独立运行用于调试：手动喂 JSON 行即可观察行为。
//
// 铁律：stdout 只允许协议消息；一切日志走 stderr。
package main

import (
	"context"
	"log/slog"
	"os"

	"aiquick/internal/backend"
	"aiquick/internal/server"
	"aiquick/internal/store"
)

// version 由 build.ps1 之外的手动构建注入位；默认 dev。
var version = "dev"

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	dir, err := store.DefaultDir()
	if err != nil {
		slog.Error("resolve config dir", "err", err)
		os.Exit(1)
	}
	st, err := store.Open(dir)
	if err != nil {
		slog.Error("open store", "dir", dir, "err", err)
		os.Exit(1)
	}

	b := backend.New(st)
	b.Version = version

	slog.Info("aiquickd serving", "dir", dir, "version", version)
	if err := server.Serve(context.Background(), os.Stdin, os.Stdout, b.Router()); err != nil {
		slog.Error("serve ended", "err", err)
		os.Exit(1)
	}
	slog.Info("aiquickd exit")
}
