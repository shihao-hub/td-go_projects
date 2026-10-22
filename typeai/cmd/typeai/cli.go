//go:build cli

// Console 通道入口（typeai-cli）：终端 TUI、管理命令与 serve 专用。
// 该产物为 console 子系统，父 shell 会正常等待进程退出，
// prompt、光标、管道与输出重定向全部符合预期。
package main

import (
	"os"

	"typeai/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:]))
}
