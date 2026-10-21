//go:build cli

// Console 通道入口（agyquota-cli）：终端 / 脚本 / MCP / Agent 专用。
// 该产物为 console 子系统，父 shell 会正常等待进程退出，
// prompt、光标、管道与输出重定向全部符合预期；它永远不开窗口。
package main

import (
	"os"

	"agyquota/internal/cli"
	"agyquota/internal/daemon"
)

func main() {
	root := cli.NewRootCmd()
	root.AddCommand(daemon.NewServeCmd())
	os.Exit(cli.Execute(root, os.Args[1:]))
}
