package cli

import (
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"

	"clictl/internal/runner"
	"clictl/internal/store"
)

// Version 版本号，唯一来源是构建命令 -ldflags -X 注入
var Version = "dev"

var st *store.Store

// mustStore 惰性打开数据库（version/help 不触发），失败输出 JSON 错误
func mustStore() *store.Store {
	if st == nil {
		s, err := store.Open()
		if err != nil {
			Fail("db_error", "打开数据库失败: "+err.Error())
		}
		st = s
	}
	return st
}

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
	if cmd != "run" {
		// run 的剩余参数全部透传给子进程，不能剥离 --pretty
		rest = stripPretty(rest)
	}

	switch cmd {
	case "add":
		return cmdAdd(rest)
	case "rm":
		return cmdRm(rest)
	case "set":
		return cmdSet(rest)
	case "list":
		return cmdList(rest)
	case "info":
		return cmdInfo(rest)
	case "run":
		return cmdRun(rest)
	case "version", "--version", "-v":
		Emit(map[string]string{"version": Version})
		return 0
	case "help", "-h", "--help":
		EmitHelp()
		return 0
	default:
		Fail("unknown_command", "未知命令: "+cmd+"，clictl help 查看用法")
		return 1
	}
}

// EmitHelp 输出帮助（也是 JSON，受 --pretty 影响）
func EmitHelp() {
	Emit(map[string]any{
		"usage": "clictl <command> [args...]",
		"global_flags": []map[string]string{
			{"flag": "--pretty", "desc": "缩进 JSON 输出；可位于子命令前后，但 run 的透传段除外"},
		},
		"commands": []map[string]string{
			{"cmd": "add <path> [--name N] [--desc D] [--meta JSON]", "desc": "注册 exe；name 默认=文件名去 .exe 小写化"},
			{"cmd": "rm <name>", "desc": "删除注册（级联删其 launches）"},
			{"cmd": "set <name> --meta JSON", "desc": "整体替换 meta（传 {} 清空）"},
			{"cmd": "list [--status active|invalid]", "desc": "全部工具，launch_count 降序"},
			{"cmd": "info <name>", "desc": "详情 + 最近 10 条启动 + 累计耗时"},
			{"cmd": "run <name> [args...]", "desc": "透传启动；退出码=子进程码，未注册/失效=127"},
			{"cmd": "version", "desc": "版本号"},
			{"cmd": "help", "desc": "本帮助"},
		},
	})
}

func cmdAdd(args []string) int {
	fs := newFlagSet("add")
	name := fs.String("name", "", "调用名，默认=文件名去 .exe 后小写")
	desc := fs.String("desc", "", "描述")
	meta := fs.String("meta", "", `扩展属性 JSON，如 {"source":"cargo","tags":["dev"]}`)
	flags, positional := splitFlags(args, map[string]bool{"name": true, "desc": true, "meta": true})
	if !parseFlags(fs, flags) {
		return 0
	}
	if len(positional) != 1 {
		Fail("bad_args", "用法: clictl add <path> [--name N] [--desc D] [--meta JSON]")
		return 1
	}
	path := positional[0]
	// v1 仅管理 .exe（.cmd/.bat shim 二期）
	if strings.ToLower(filepath.Ext(path)) != ".exe" {
		Fail("not_exe", "v1 仅支持注册 .exe 文件: "+path)
		return 1
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		Fail("bad_args", "路径无效: "+err.Error())
		return 1
	}
	if fi, err := os.Stat(abs); err != nil || fi.IsDir() {
		Fail("file_not_found", "文件不存在: "+abs)
		return 1
	}

	finalName := store.NormalizeName(*name)
	if finalName == "" {
		base := filepath.Base(abs)
		finalName = store.NormalizeName(strings.TrimSuffix(base, filepath.Ext(base)))
	}
	if finalName == "" || strings.ContainsAny(finalName, " \t/\\") {
		Fail("bad_args", "非法的 name: "+*name)
		return 1
	}

	var metaRaw []byte
	if strings.TrimSpace(*meta) != "" {
		metaRaw = []byte(*meta)
	}

	tool, err := mustStore().AddTool(finalName, abs, *desc, metaRaw)
	if err != nil {
		failFromErr("add", err)
		return 1
	}
	Emit(tool)
	return 0
}

func cmdRm(args []string) int {
	if len(args) != 1 {
		Fail("bad_args", "用法: clictl rm <name>")
		return 1
	}
	name := store.NormalizeName(args[0])
	n, err := mustStore().RemoveTool(name)
	if err != nil {
		failFromErr("rm", err)
		return 1
	}
	Emit(map[string]any{"removed": n, "name": name})
	return 0
}

func cmdSet(args []string) int {
	fs := newFlagSet("set")
	meta := fs.String("meta", "", "扩展属性 JSON（整体替换，传 {} 清空）")
	flags, positional := splitFlags(args, map[string]bool{"meta": true})
	if !parseFlags(fs, flags) {
		return 0
	}
	if len(positional) != 1 || strings.TrimSpace(*meta) == "" {
		Fail("bad_args", "用法: clictl set <name> --meta JSON")
		return 1
	}
	tool, err := mustStore().SetMeta(store.NormalizeName(positional[0]), []byte(*meta))
	if err != nil {
		failFromErr("set", err)
		return 1
	}
	Emit(tool)
	return 0
}

func cmdList(args []string) int {
	fs := newFlagSet("list")
	status := fs.String("status", "", "按状态过滤: active | invalid")
	flags, _ := splitFlags(args, map[string]bool{"status": true})
	if !parseFlags(fs, flags) {
		return 0
	}
	if *status != "" && *status != store.StatusActive && *status != store.StatusInvalid {
		Fail("bad_args", "--status 仅支持 active | invalid")
		return 1
	}
	tools, err := mustStore().ListTools(*status)
	if err != nil {
		failFromErr("list", err)
		return 1
	}
	Emit(tools)
	return 0
}

func cmdInfo(args []string) int {
	if len(args) != 1 {
		Fail("bad_args", "用法: clictl info <name>")
		return 1
	}
	s := mustStore()
	tool, err := s.GetTool(store.NormalizeName(args[0]))
	if err != nil {
		failFromErr("info", err)
		return 1
	}
	launches, err := s.RecentLaunches(tool.ID, 10)
	if err != nil {
		failFromErr("info", err)
		return 1
	}
	finished, totalMs, err := s.TotalDuration(tool.ID)
	if err != nil {
		failFromErr("info", err)
		return 1
	}
	Emit(map[string]any{
		"tool":              tool,
		"recent_launches":   launches,
		"finished_count":    finished,
		"total_duration_ms": totalMs,
	})
	return 0
}

func cmdRun(args []string) int {
	if len(args) < 1 {
		FailStderr("bad_args", "用法: clictl run <name> [args...]", 1)
		return 1
	}
	code, err := runner.Run(mustStore(), store.NormalizeName(args[0]), args[1:])
	if err != nil {
		var re *runner.RunError
		if errors.As(err, &re) {
			FailStderr(re.Code, re.Message, re.ExitCode)
			return re.ExitCode
		}
		FailStderr("internal", err.Error(), 1)
		return 1
	}
	return code
}

// failFromErr 把 store 领域错误映射为统一的 JSON 错误码
func failFromErr(prefix string, err error) {
	var metaErr *store.MetaError
	if errors.As(err, &metaErr) {
		Fail(metaErr.Code, metaErr.Message)
		return
	}
	var confErr *store.ConflictError
	if errors.As(err, &confErr) {
		switch confErr.Field {
		case "name":
			Fail("conflict", "该 name 已被其他工具使用")
		case "path":
			Fail("conflict", "该 exe 路径已注册为其他工具")
		default:
			Fail("conflict", "唯一性冲突: "+confErr.Field)
		}
		return
	}
	if errors.Is(err, store.ErrNotFound) {
		Fail("not_found", prefix+": 未找到该工具")
		return
	}
	Fail("internal", prefix+": "+err.Error())
}

func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard) // 默认 usage 输出丢弃，统一走 JSON 错误
	return fs
}

// parseFlags 解析 flag 段；遇 -h/--help（flag.ErrHelp）输出整体帮助，
// 其余解析错误输出 bad_args（Fail 内部已退出进程）。返回 false 表示已处理完毕，
// 调用方直接以退出码 0 返回（只有 help 路径会真正走到这里）。
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
// "遇到第一个位置参数即停止解析"的限制，支持 `add path --name N` 混合顺序。
// 约定：本项目所有命令 flag 均带值（--key value 或 --key=value），无布尔 flag。
func splitFlags(args []string, known map[string]bool) (flags []string, positional []string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a != "-" && strings.HasPrefix(a, "-") {
			name := strings.TrimLeft(a, "-")
			if strings.Contains(name, "=") {
				flags = append(flags, a) // --key=value 自带值
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
