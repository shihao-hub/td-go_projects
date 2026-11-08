//go:build !cli

// GUI 通道入口（deepseeksearch-gui）：只负责启动桌面工作台。
// 它不承担任何 CLI 语义：不管从哪里启动（双击、终端、快捷方式、带任意参数），
// 行为都是显示 GUI。唯一有意义的参数是 --hidden（开机自启注册的静默托盘模式，
// --tray 为同义别名），其余参数一律忽略。
// 需要命令行能力请改用 console 通道产物 deepseeksearch-cli.exe。
package main

import (
	"fmt"
	"os"

	"deepseeksearch/internal/api"
	"deepseeksearch/internal/app"
	"deepseeksearch/internal/storage"
)

func main() {
	defer app.Recover(true)

	hidden := app.IsSilentStart(os.Args)

	if err := app.LaunchGUI("", 0, hidden); err != nil {
		fallbackServe(!hidden)
	}
}

func fallbackServe(openBrowser bool) {
	dbPath := app.DefaultDBPath()
	store, err := storage.NewStore(dbPath)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "打开数据库失败: %v\n", err)
		return
	}
	defer store.Close()

	server := api.NewServer(store, api.Meta{
		Channel:   app.Channel(),
		BuildMode: app.BuildMode,
		Version:   app.Version,
	})
	if _, err := server.Start(0, openBrowser); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "服务异常终止: %v\n", err)
	}
}
