// liteconf：liteconf 配置中心的调试 CLI。
// 子命令 http 把参数原样透传给 curlie；schema 导出 CLI 契约目录。
// 本文件只做组装：装配标准流与退出码，无业务逻辑。
package main

import (
	"os"

	"github.com/shihao-hub/liteconf/internal/cli"
)

func main() {
	opt := cli.Options{
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}
	os.Exit(cli.Run(os.Args[1:], opt))
}
