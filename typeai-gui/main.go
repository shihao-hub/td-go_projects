// typeai-gui：typeai 的桌面终端窗口壳（Wails v3 + xterm.js + ConPTY）。
// 窗口内即原版 TUI：壳不重画任何界面，typeai.exe 零改动。
package main

import (
	"embed"

	"typeai-gui/internal/guiapp"
)

// 前端构建产物（vite build 生成 frontend/dist）。
//
//go:embed all:frontend/dist
var assets embed.FS

func main() {
	guiapp.Run(guiapp.RunOptions{Assets: assets})
}
