// glmquotawatch-gui：GLM 编码套餐用量监控 GUI（Wails v3 + 托盘常驻）。
// 入口分流：args 为空或全部属于 {--demo, --hidden} → GUI；
// 其余（含 --help/--version/未知 flag/子命令）→ CLI 壳（恒 JSON 信封）。
package main

import (
	"embed"
	"os"

	"glmquotawatch-gui/internal/cli"
	"glmquotawatch-gui/internal/guiapp"
)

// 前端构建产物（wails3 build / npm run build 生成 frontend/dist）。
//
//go:embed all:frontend/dist
var assets embed.FS

// 项目根图标字节（托盘图标；exe 图标由 build/windows/icon.ico 生成）。
//
//go:embed icon.ico
var iconICO []byte

func main() {
	args := os.Args[1:]

	demo, hidden, gui := false, false, true
	for _, a := range args {
		switch a {
		case "--demo":
			demo = true
		case "--hidden":
			hidden = true
		default: // 未知 flag 或子命令 → CLI 路径（cobra 信封处理）
			gui = false
		}
	}
	if gui {
		guiapp.Run(guiapp.RunOptions{Demo: demo, Hidden: hidden, Assets: assets, Icon: iconICO})
		return
	}
	os.Exit(cli.Run(args))
}
