// Package cli 实现 liteconf 调试 CLI 的子命令分发与执行编排。
// 命令函数只返回退出码，由组装入口（cmd/liteconf）统一 os.Exit。
package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/shihao-hub/liteconf/internal/version"
)

// Options 汇总 CLI 运行依赖；零值可用（回退进程标准流与 exec.LookPath），
// 测试通过替换字段注入桩实现。
type Options struct {
	Stdin    io.Reader
	Stdout   io.Writer
	Stderr   io.Writer
	LookPath func(file string) (string, error)
}

// defaults 回填未显式注入的依赖。
func (o *Options) defaults() {
	if o.Stdin == nil {
		o.Stdin = os.Stdin
	}
	if o.Stdout == nil {
		o.Stdout = os.Stdout
	}
	if o.Stderr == nil {
		o.Stderr = os.Stderr
	}
	if o.LookPath == nil {
		o.LookPath = exec.LookPath
	}
}

// Run 按首参数分发子命令并返回进程退出码：
// 0 成功；1 运行失败（如 curlie 缺失）；2 调用参数错误。
func Run(args []string, opt Options) int {
	opt.defaults()
	if len(args) == 0 {
		return renderHelp(&opt)
	}
	switch args[0] {
	case "-h", "--help", "help":
		return renderHelp(&opt)
	case "--version", "version":
		fmt.Fprintln(opt.Stdout, version.Version)
		return 0
	case "http":
		return runHTTP(args[1:], &opt)
	case "schema":
		return runSchema(&opt)
	default:
		fmt.Fprintf(opt.Stderr, "liteconf: 未知子命令 %q\n执行 liteconf --help 查看用法\n", args[0])
		return 2
	}
}
