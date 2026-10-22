//go:build !cli

// GUI 通道入口（liteconf-gui）：只负责拉起桌面控制台。
//
// 它不承担任何 CLI 语义：不管从哪里启动（双击、终端、快捷方式、带任意参数），
// 行为都是显示 GUI。唯一有意义的参数是 --hidden（开机自启注册的静默托盘模式，
// --tray 为同义别名），其余参数一律忽略。
// 需要命令行能力请改用 console 通道产物 liteconf-cli.exe。
package main

import (
	"fmt"
	"os"

	"github.com/shihao-hub/liteconf/internal/app"
)

func main() {
	defer app.Recover(true)

	hidden := false
	for _, arg := range os.Args[1:] {
		switch arg {
		case "--hidden", "--tray":
			hidden = true
		}
	}

	if err := app.LaunchGUI("", 0, hidden); err != nil {
		// 桌面壳起不来（典型原因：WebView2 运行时缺失）时降级为无窗口 HTTP 服务，
		// 至少还能用浏览器打开控制台；诊断信息已由 appenv.Logf 落盘。
		// 静默自启场景不弹浏览器，避免开机时打扰用户。
		if fallbackErr := app.ServeFallback("", 0, !hidden); fallbackErr != nil {
			_, _ = fmt.Fprintf(os.Stderr, "liteconf 启动失败: %v（降级服务也失败: %v）\n", err, fallbackErr)
		}
	}
}
