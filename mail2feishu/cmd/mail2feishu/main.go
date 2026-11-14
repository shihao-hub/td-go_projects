//go:build !cli

// GUI 通道入口：启动完整服务（IMAP 监听 + HTTP API + WebUI + 托盘）。
//
// 唯一有意义的参数是 --hidden（或 --tray），用于开机自启时只显示托盘。
package main

import (
	"flag"
	"fmt"
	"os"

	"mail2feishu/internal/app"
)

func main() {
	hidden := flag.Bool("hidden", false, "只显示托盘，不打开主窗口（开机自启使用）")
	tray := flag.Bool("tray", false, "同 --hidden")
	flag.Parse()

	if err := app.Launch(0, *hidden || *tray); err != nil {
		app.ShowFatal("mail2feishu 启动失败", err.Error())
		fmt.Fprintf(os.Stderr, "mail2feishu: %v\n", err)
		os.Exit(1)
	}
}
