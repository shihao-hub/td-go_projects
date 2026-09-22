// agyquota：Antigravity 模型配额查询 CLI（无需打开 IDE）。
package main

import (
	"os"

	"agyquota/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:]))
}
