package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"typeai/internal/config"
	"typeai/internal/llm"
	"typeai/internal/service"
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
	fmt.Printf("model: %s\nsession: %s\n/exit 退出\n", chat.Model(), chat.Path())

	reader := bufio.NewReader(os.Stdin)
	seenReasoning := false
	for {
		fmt.Print("> ")
		line, err := reader.ReadString('\n')
		if err != nil && line == "" {
			if err == io.EOF {
				return 0
			}
			fmt.Fprintln(os.Stderr, "typeai: 读取输入失败:", err)
			return 1
		}
		input := strings.TrimSpace(line)
		if input == "" {
			continue
		}
		if input == "/exit" || input == "/quit" {
			return 0
		}
		if err := chat.Send(context.Background(), input, func(delta llm.Delta) {
			switch delta.Kind {
			case llm.DeltaReasoning:
				seenReasoning = true
				fmt.Print(delta.Text)
			case llm.DeltaAnswer:
				if seenReasoning {
					fmt.Print("\n\n")
					seenReasoning = false
				}
				fmt.Print(delta.Text)
			}
		}); err != nil {
			fmt.Fprintf(os.Stderr, "\ntypeai: %v\n\n", err)
			continue
		}
		fmt.Print("\n\n")
	}
}
