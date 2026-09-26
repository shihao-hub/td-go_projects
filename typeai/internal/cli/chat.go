package cli

import (
	"fmt"
	"os"
	"time"

	"typeai/internal/config"
	"typeai/internal/service"
	"typeai/internal/tui"
)

func runChat() int {
	dir, err := config.DefaultDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "typeai:", err)
		return 1
	}
	cfg, err := config.Load(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "typeai:", err)
		return 1
	}
	if err := config.Validate(cfg); err != nil {
		fmt.Fprintln(os.Stderr, "typeai:", err)
		return 1
	}
	if cfg.APIKey == "" {
		fmt.Fprintln(os.Stderr, "typeai: 尚未配置 API Key，请先执行 typeai config set --api-key <KEY>")
		return 1
	}

	chat, err := service.NewChat(cfg, dir, time.Now())
	if err != nil {
		fmt.Fprintln(os.Stderr, "typeai:", err)
		return 1
	}
	if err := tui.Run(chat, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "typeai:", err)
		return 1
	}
	return 0
}
