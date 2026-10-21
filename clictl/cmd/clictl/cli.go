//go:build cli

// Console 通道入口（clictl-cli）：终端 / 脚本 / Agent 专用。
//
// 该产物为 console 子系统，父 shell 正常等待进程退出，prompt、光标、
// 管道与输出重定向全部符合预期；GUI 通道产物不解析任何子命令。
package main

import (
	"os"

	"clictl/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:]))
}
