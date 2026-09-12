// Package cli 实现子命令分发与各命令逻辑（结构对齐 clictl 的 CLI 骨架模式）。
package cli

import (
	"errors"
	"flag"
	"io"
	"os"
	"strings"

	"zreadmanager/internal/browse"
	"zreadmanager/internal/store"
)

// Run 分发子命令，返回进程退出码
func Run(args []string) int {
	// 子命令名之前的全局 --pretty
	for len(args) > 0 && args[0] == "--pretty" {
		Pretty = true
		args = args[1:]
	}
	if len(args) == 0 {
		EmitHelp()
		return 0
	}

	cmd, rest := args[0], args[1:]
	rest = stripPretty(rest) // 全部是管理命令，无透传段

	switch cmd {
	case "start":
		return cmdStart(rest)
	case "stop":
		return cmdStop(rest)
	case "restart":
		return cmdRestart(rest)
	case "status":
		return cmdStatus(rest)
	case "help", "-h", "--help":
		EmitHelp()
		return 0
	default:
		Fail("unknown_command", "未知命令: "+cmd+"，zreadmanager help 查看用法")
		return 1
	}
}

// EmitHelp 输出帮助（也是 JSON，受 --pretty 影响）
func EmitHelp() {
	Emit(map[string]any{
		"usage": "zreadmanager <command> [args...]",
		"global_flags": []map[string]string{
			{"flag": "--pretty", "desc": "缩进 JSON 输出，可位于子命令前后"},
		},
		"commands": []map[string]string{
			{"cmd": "start [--dir D] [--host H] [--port P] [--generate]", "desc": "后台启动 zread browse 并记住工作区；--dir 缺省用上次记录；已有活实例报 conflict"},
			{"cmd": "stop", "desc": "树杀活实例并清理运行记录（无活实例时幂等返回 stopped=false）"},
			{"cmd": "restart", "desc": "用上次参数重启（无记录时需先 start）"},
			{"cmd": "status", "desc": "查看运行状态（PID 探活 + 创建时间比对防误判）"},
			{"cmd": "help", "desc": "本帮助"},
		},
	})
}

// cmdStart 后台分离启动；--dir 解析优先级：参数 > 上次记录 > 报错
func cmdStart(args []string) int {
	fs := newFlagSet("start")
	dir := fs.String("dir", "", "zread 工作区目录（缺省用上次记录）")
	host := fs.String("host", "", "zread browse 监听地址（默认由 zread 决定）")
	port := fs.Int("port", 0, "监听端口（0 表示由 zread 从 9681 起自动探测）")
	generate := fs.Bool("generate", false, "工作区无文档时直接启动生成流程而非显示菜单")
	flags, _ := splitFlags(args, map[string]bool{"dir": true, "host": true, "port": true}, map[string]bool{"generate": true})
	if !parseFlags(fs, flags) {
		return 0
	}

	target := *dir
	if target == "" {
		target = store.Load().LastDir
	}
	if target == "" {
		Fail("bad_args", "未指定 --dir 且无上次工作区记录")
		return 1
	}
	info, err := os.Stat(target)
	if err != nil || !info.IsDir() {
		Fail("bad_args", "工作区目录不存在: "+target)
		return 1
	}

	r, err := browse.Start(browse.Options{
		Dir:      target,
		Host:     *host,
		Port:     *port,
		Generate: *generate,
	})
	if err != nil {
		failFromErr("start", err)
		return 1
	}
	// 记住工作区（失败不影响启动结果，与原 zread-tray 行为一致）
	_ = store.Save(store.Config{LastDir: r.Dir})
	Emit(r)
	return 0
}

// cmdStop 树杀 + 清理记录，幂等
func cmdStop(args []string) int {
	if !warnExtraArgs("stop", args) {
		return 1
	}
	old, stopped, err := browse.Stop()
	if err != nil {
		failFromErr("stop", err)
		return 1
	}
	if !stopped {
		Emit(map[string]any{"stopped": false, "reason": "not_running"})
		return 0
	}
	Emit(map[string]any{"stopped": true, "pid": old.Pid, "dir": old.Dir})
	return 0
}

// cmdRestart 用 pidfile 中的上次参数重启
func cmdRestart(args []string) int {
	if !warnExtraArgs("restart", args) {
		return 1
	}
	r, err := browse.Restart()
	if err != nil {
		failFromErr("restart", err)
		return 1
	}
	_ = store.Save(store.Config{LastDir: r.Dir})
	Emit(r)
	return 0
}

// cmdStatus 探活并输出运行状态
func cmdStatus(args []string) int {
	if !warnExtraArgs("status", args) {
		return 1
	}
	r, running := browse.Status()
	if !running {
		Emit(map[string]any{"running": false})
		return 0
	}
	Emit(map[string]any{
		"running":    true,
		"pid":        r.Pid,
		"dir":        r.Dir,
		"host":       r.Host,
		"port":       r.Port,
		"started_at": r.StartedAt,
	})
	return 0
}

// failFromErr 领域错误 → JSON 错误码单点映射
func failFromErr(prefix string, err error) {
	var already *browse.AlreadyRunningError
	if errors.As(err, &already) {
		Fail("conflict", prefix+": "+already.Error())
		return
	}
	switch {
	case errors.Is(err, browse.ErrZreadNotFound):
		Fail("not_found", prefix+": "+err.Error())
	case errors.Is(err, browse.ErrNoLastOptions):
		Fail("not_found", prefix+": "+err.Error())
	default:
		Fail("internal", prefix+": "+err.Error())
	}
}

// warnExtraArgs 不接受额外参数的命令统一在此报错
func warnExtraArgs(name string, args []string) bool {
	if len(args) > 0 {
		Fail("bad_args", name+" 不接受参数: "+strings.Join(args, " "))
		return false
	}
	return true
}

func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard) // 默认 usage 输出丢弃，统一走 JSON 错误
	return fs
}

// parseFlags 解析 flag 段；遇 -h/--help（flag.ErrHelp）输出整体帮助，
// 其余解析错误输出 bad_args。返回 false 表示已处理完毕，调用方以退出码 0 返回。
func parseFlags(fs *flag.FlagSet, flags []string) bool {
	if err := fs.Parse(flags); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			EmitHelp()
			return false
		}
		Fail("bad_args", fs.Name()+": "+err.Error())
		return false
	}
	return true
}

// splitFlags 把 args 拆成 (flag 段, 位置参数段)，绕开 Go flag
// "遇到第一个位置参数即停止解析"的限制，支持混合顺序。
// known：带值 flag（--key value / --key=value）；knownBool：布尔 flag（--key / --key=true）
func splitFlags(args []string, known map[string]bool, knownBool map[string]bool) (flags []string, positional []string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a != "-" && strings.HasPrefix(a, "-") {
			name := strings.TrimLeft(a, "-")
			if strings.Contains(name, "=") {
				flags = append(flags, a) // --key=value 自带值
				continue
			}
			if knownBool[name] {
				flags = append(flags, a) // 布尔 flag 单独出现即可
				continue
			}
			if known[name] && i+1 < len(args) {
				flags = append(flags, a, args[i+1]) // --key value
				i++
				continue
			}
			flags = append(flags, a) // 未知/缺值，交给 flag.Parse 报错
			continue
		}
		positional = append(positional, a)
	}
	return flags, positional
}

func stripPretty(args []string) []string {
	out := make([]string, 0, len(args))
	for _, a := range args {
		if a == "--pretty" {
			Pretty = true
			continue
		}
		out = append(out, a)
	}
	return out
}
