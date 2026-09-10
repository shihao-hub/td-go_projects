// clictl：Windows 单文件 CLI 工具注册器/启动器。
package main

import (
	"os"

	"clictl/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:]))
}
