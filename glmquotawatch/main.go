// glmquotawatch：GLM 编码套餐用量采样与阈值告警 CLI。
package main

import (
	"os"

	"glmquotawatch/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:]))
}
