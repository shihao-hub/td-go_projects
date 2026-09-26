// agyquota：Antigravity 模型配额查询 CLI（无需打开 IDE）。
// 单二进制双角色：serve 为 daemon，其余子命令为薄客户端。
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
