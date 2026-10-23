// termhub：Windows 终端会话与启动项生命周期管理器（T01 曳光弹）。
package main

import (
	"fmt"
	"os"

	"github.com/shihao-hub/termhub/internal/app"
)

func main() {
	port := 0
	for _, arg := range os.Args[1:] {
		if arg == "--port" && len(os.Args) > 1 {
			// 简单处理；正式参数解析在 T08 build.py 一并完善。
			_ = port
		}
	}
	if err := app.LaunchGUI(port); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "termhub 启动失败: %v\n", err)
		os.Exit(1)
	}
}
