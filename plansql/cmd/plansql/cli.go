//go:build cli

// Console 通道入口（plansql-cli）：终端 / 脚本 / Agent / MCP 专用。
// 该产物为 console 子系统，父 shell 会正常等待进程退出，
// prompt、光标、管道与输出重定向全部符合预期。
package main

import (
	"flag"
	"fmt"
	"os"
	"plansql/internal/cli"
	"plansql/internal/mcp"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "mcp":
			fs := flag.NewFlagSet("mcp", flag.ExitOnError)
			host := fs.String("host", "", "daemon host")
			_ = fs.Parse(os.Args[2:])
			if err := mcp.RunMCPServer(*host); err != nil {
				fmt.Fprintf(os.Stderr, "mcp error: %v\n", err)
				os.Exit(1)
			}
			return
		case "schema":
			mcp.ExportSchema()
			return
		}
	}

	exitCode := cli.RunCLI(os.Args[1:])
	os.Exit(exitCode)
}
