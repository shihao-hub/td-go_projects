package cli

import (
	"fmt"
	"os"
)

// Version 由构建脚本通过 -ldflags 注入，开发构建保持 dev。
var Version = "dev"

// Run 解析 CLI 参数并返回进程退出码。
func Run(args []string) int {
	if len(args) == 0 {
		return runChat()
	}

	switch args[0] {
	case "help", "-h", "--help":
		emitHelp()
		return 0
	case "version", "--version":
		return cmdVersion(args[1:])
	case "schema", "--schema":
		return cmdSchema(args[1:])
	case "config":
		return cmdConfig(args[1:])
	default:
		fmt.Fprintln(os.Stderr, "typeai: 未知命令:", args[0])
		fmt.Fprintln(os.Stderr, "用法: typeai help")
		return 1
	}
}

func cmdVersion(args []string) int {
	jsonMode, err := onlyJSONFlag(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "typeai:", err)
		return 2
	}
	data := map[string]string{"name": "typeai", "version": Version}
	if jsonMode {
		return emitJSON(0, data)
	}
	fmt.Println("typeai", Version)
	return 0
}
