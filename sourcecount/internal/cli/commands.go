// Package cli 实现 sourcecount 的命令解析和输出适配。
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"

	"sourcecount/internal/contract"
	"sourcecount/internal/mcp"
	"sourcecount/internal/service"
)

// Version 是唯一版本注入点，默认值用于开发构建。
var Version = "dev"

type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }
func (s *stringList) Set(value string) error {
	if strings.TrimSpace(value) == "" {
		return errors.New("值不能是空字符串")
	}
	*s = append(*s, value)
	return nil
}

// Run 分发命令并返回进程退出码。
func Run(args []string) int {
	if len(args) == 0 {
		return runScan(nil)
	}
	switch args[0] {
	case "mcp":
		if len(args) != 1 {
			fmt.Fprintln(os.Stderr, "用法: sourcecount mcp")
			return 2
		}
		mcp.Version = Version
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()
		if err := mcp.Run(ctx); err != nil {
			fmt.Fprintln(os.Stderr, "MCP server 异常退出:", err)
			return 1
		}
		return 0
	case "schema", "--schema":
		if len(args) != 1 {
			fmt.Fprintln(os.Stderr, "用法: sourcecount schema")
			return 2
		}
		mcp.Version = Version
		tools, err := mcp.Schema(context.Background())
		if err != nil {
			fmt.Fprintln(os.Stderr, "导出 Schema 失败:", err)
			return 1
		}
		if err := writeJSON(os.Stdout, map[string]any{"name": "sourcecount", "version": Version, "tools": tools}, true); err != nil {
			fmt.Fprintln(os.Stderr, "写入 Schema 失败:", err)
			return 1
		}
		return 0
	case "help", "-h", "--help":
		if len(args) != 1 {
			fmt.Fprintln(os.Stderr, "用法: sourcecount help")
			return 2
		}
		printHelp(os.Stdout)
		return 0
	case "version", "-v", "--version":
		if len(args) != 1 {
			fmt.Fprintln(os.Stderr, "用法: sourcecount --version")
			return 2
		}
		fmt.Fprintln(os.Stdout, Version)
		return 0
	default:
		return runScan(args)
	}
}

func runScan(args []string) int {
	fs := flag.NewFlagSet("sourcecount", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	jsonMode := fs.Bool("json", false, "输出 JSON")
	pretty := fs.Bool("pretty", false, "缩进 JSON 输出")
	configPath := fs.String("config", "", "JSON 配置文件路径")
	noDefaultExcludes := fs.Bool("no-default-excludes", false, "关闭内置默认排除目录")
	var includes, excludes, textExtensions, binaryExtensions stringList
	fs.Var(&includes, "include", "覆盖配置 include，可重复")
	fs.Var(&excludes, "exclude", "覆盖配置 exclude，可重复")
	fs.Var(&textExtensions, "text-ext", "强制文本后缀，可重复")
	fs.Var(&binaryExtensions, "binary-ext", "强制二进制后缀，可重复")
	if err := fs.Parse(args); err != nil {
		if *jsonMode {
			_ = writeJSON(os.Stdout, envelope{OK: false, Error: &errorBody{Code: "bad_args", Message: err.Error()}}, *pretty)
		} else {
			fmt.Fprintln(os.Stderr, "参数错误:", err)
		}
		return 2
	}

	report, err := service.New().Scan(context.Background(), contract.ScanRequest{
		Roots:             fs.Args(),
		ConfigPath:        *configPath,
		Include:           optionalList(includes),
		Exclude:           optionalList(excludes),
		TextExtensions:    optionalList(textExtensions),
		BinaryExtensions:  optionalList(binaryExtensions),
		NoDefaultExcludes: *noDefaultExcludes,
	})
	if err != nil {
		return emitServiceError(err, *jsonMode, *pretty)
	}
	return emitReport(report, *jsonMode, *pretty)
}

func optionalList(values []string) []string {
	if values == nil {
		return nil
	}
	return append([]string(nil), values...)
}

func printHelp(w io.Writer) {
	fmt.Fprintln(w, "sourcecount - 项目源码统计工具")
	fmt.Fprintln(w, "用法: sourcecount [选项] [根路径 ...]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "命令:")
	fmt.Fprintln(w, "  sourcecount schema       导出 MCP 工具 Schema")
	fmt.Fprintln(w, "  sourcecount mcp          启动 stdio MCP server")
	fmt.Fprintln(w, "  sourcecount --version    输出版本号")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "选项:")
	fmt.Fprintln(w, "  --config PATH             指定 JSON 配置文件")
	fmt.Fprintln(w, "  --include GLOB            覆盖配置 include，可重复")
	fmt.Fprintln(w, "  --exclude GLOB            覆盖配置 exclude，可重复")
	fmt.Fprintln(w, "  --text-ext EXT            强制文本后缀，可重复")
	fmt.Fprintln(w, "  --binary-ext EXT          强制二进制后缀，可重复")
	fmt.Fprintln(w, "  --no-default-excludes     关闭内置排除目录")
	fmt.Fprintln(w, "  --json                    输出机器可读 JSON")
	fmt.Fprintln(w, "  --pretty                  缩进 JSON")
	fmt.Fprintln(w, "  --help                    显示帮助")
}
