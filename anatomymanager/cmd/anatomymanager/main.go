//go:build !cli

// GUI 通道入口（anatomymanager-gui）：只负责把桌面工作台拉起来。
//
// 它不承担任何 CLI 语义：不管从哪里启动（双击、终端、快捷方式、带任意参数），
// 行为都是显示 GUI。唯一有意义的参数是 --hidden（开机自启注册的静默托盘模式，
// --tray 为同义别名），其余参数一律忽略。
// 需要命令行能力请改用 console 通道产物 anatomymanager-cli.exe。
package main

import (
	"fmt"
	"os"

	"anatomymanager/internal/api"
	"anatomymanager/internal/app"
	"anatomymanager/internal/storage"
)

func main() {
	hidden := false
	for _, arg := range os.Args[1:] {
		switch arg {
		case "--hidden", "--tray":
			hidden = true
		}
	}

	if err := app.LaunchGUI("", 0, hidden); err != nil {
		// 桌面壳起不来（典型原因：WebView2 运行时缺失）时降级为无窗口 HTTP 服务，
		// 至少还能用浏览器打开工作台；诊断信息已由 app.logf 落盘。
		// 静默自启场景不弹浏览器，避免开机时打扰用户。
		fallbackServe(!hidden)
	}
}

// fallbackServe 桌面壳不可用时的兜底：起内置 HTTP 服务，可选自动打开浏览器。
func fallbackServe(openBrowser bool) {
	store, err := storage.NewStore(app.DefaultDBPath())
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "打开数据库失败: %v\n", err)
		return
	}
	defer store.Close()

	server := api.NewServer(store)
	if _, err := server.Start(0, openBrowser); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "服务异常终止: %v\n", err)
	}
}
