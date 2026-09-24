// glmquotawatch-gui：GLM 编码套餐用量监控 GUI（Wails v3 + 托盘常驻）。
// 入口分流：args 为空或全部属于 {--demo, --hidden} → GUI；其余 → CLI（恒 JSON）。
package main

import (
	"os"

	"glmquotawatch-gui/internal/cli"
)

func main() {
	args := os.Args[1:]
	// TODO(任务 6): args 空或全部 ∈ {--demo,--hidden} 时进入 guiapp.Run(demo, hidden)；
	// 当前阶段先全部落 CLI（含信封 help），保证可构建可用。
	os.Exit(cli.Run(args))
}
