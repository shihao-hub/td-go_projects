//go:build cli

// Console 通道入口（liteconf-cli）：终端 / 脚本 / Agent 专用。
// 该产物为 console 子系统，父 shell 会正常等待进程退出，
// prompt、光标、管道与输出重定向全部符合预期。
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
