// sourcecount 是项目源码统计 CLI。
package main

import (
	"os"

	"sourcecount/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:]))
}
