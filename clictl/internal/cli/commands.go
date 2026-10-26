package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"

	"clictl/internal/appinfo"
	"clictl/internal/mcp"
	"clictl/internal/runner"
	"clictl/internal/service"
	"clictl/internal/store"
)

var svc *service.Service

// mustService 惰性打开业务服务（version/help/completion 脚本输出不触发），
// 失败输出 JSON 错误
func mustService() *service.Service {
	if svc == nil {
		s, err := service.Open()
		if err != nil {
			Fail("db_error", "打开数据库失败: "+err.Error())
		}
		svc = s
	}
	return svc
}

// Run 分发子命令，返回进程退出码
func Run(args []string) int {
	// 内部 helper：显示/隐藏目标控制台，不打开数据库
	if len(args) > 0 && args[0] == "__console-ctl" {
		return runner.RunConsoleCtl(args[1:])
	}

	// 子命令名之前的全局 flag（--pretty / --ascii）
	for len(args) > 0 && isGlobalFlag(args[0]) {
		setGlobalFlag(args[0])
		args = args[1:]
	}
	if len(args) == 0 {
		EmitHelp()
		return 0
	}

	// console 通道没有桌面壳可拉：--hidden / --tray 退化为无窗口常驻服务。
	if args[0] == "--hidden" || args[0] == "--tray" {
		return cmdServe(nil)
	}

	cmd, rest := args[0], args[1:]
	// run/start 的剩余参数全部透传给子进程，不能剥离全局 flag，其他的都给剥离掉
	if cmd != "run" && cmd != "start" {
		rest = stripGlobalFlags(rest)
	}

	switch cmd {
	case "add":
		return cmdAdd(rest)
	case "add-cmd":
		return cmdAddCmd(rest)
	case "edit":
		return cmdEdit(rest)
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
	case "start":
		return cmdStart(rest)
	case "stop":
		return cmdStop(rest)
	case "console":
		return cmdConsole(rest)
	case "cp":
		return cmdCp(rest)
	case "serve":
		return cmdServe(rest)
	case "where":
		return cmdWhere(rest)
	case "completion":
		return cmdCompletion(rest)
	case "mcp":
		return cmdMcp(rest)
	case "schema":
		return cmdSchema(rest)
	case "version", "--version", "-v":
		Emit(map[string]string{
			"version":    appinfo.Version,
			"channel":    appinfo.Channel(),
			"build_mode": appinfo.BuildMode,
		})
		return 0
	case "help", "-h", "--help":
		EmitHelp()
		return 0
	default:
		Fail("unknown_command", "未知命令: "+cmd+"，clictl help 查看用法")
		return 1
	}
}

// cmdMcp 启动 stdio MCP server（常驻；协议 stdout 只走 SDK 通道，
// 日志全部 stderr）。不解析 flag——参数只属于 MCP 客户端的 stdin/stdout 对话。
func cmdMcp(args []string) int {
	if len(args) > 0 {
		fmt.Fprintln(os.Stderr, "clictl mcp 不接受参数（MCP 客户端经 stdin/stdout 对话）")
		return 1
	}
	mcp.Version = appinfo.Version
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := mcp.Run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "mcp server 异常退出: "+err.Error())
		return 1
	}
	return 0
}

// cmdSchema 导出与 tools/list 同源的工具定义（含 JSON Schema），离线检查用；
// 与 help 一样不打开业务数据库。
func cmdSchema(args []string) int {
	if len(args) > 0 {
		Fail("bad_args", "用法: clictl schema")
		return 1
	}
	tools, err := mcp.Schema(context.Background())
	if err != nil {
		Fail("internal", "导出工具定义失败: "+err.Error())
		return 1
	}
	Emit(map[string]any{
		"name":    "clictl",
		"version": appinfo.Version,
		"tools":   tools,
	})
	return 0
}

// failService 把 service.Error 按输出去向输出并退出：
// toStderr=true 走 stderr（run 类前置失败），false 走 stdout（管理命令与
// start/stop——stdout 无子进程归属问题但退出码须保持 127）。
// ExitCode 尊重 service.Error（127 等），未指定默认 1。
func failService(err error, toStderr bool) int {
	var se *service.Error
	if !errors.As(err, &se) {
		se = &service.Error{Code: "internal", Message: err.Error()}
	}
	code := se.ExitCode
	if code == 0 {
		code = 1
	}
	if toStderr {
		FailStderr(se.Code, se.Message, code, se.Suggestions...)
	} else {
		FailExit(se.Code, se.Message, code, se.Suggestions...)
	}
	return code
}

// EmitHelp 输出帮助（也是 JSON，受 --pretty 影响）
func EmitHelp() {
	Emit(map[string]any{
		"usage": "clictl <command> [args...]",
		"global_flags": []map[string]string{
			{"flag": "--pretty", "desc": "缩进 JSON 输出；可位于子命令前后，但 run 的透传段除外"},
			{"flag": "--ascii", "desc": "非 ASCII 转义为 \\uXXXX（PS 5.1 管道等编码不可靠环境用，下游 JSON.parse 自动还原）；位置规则同 --pretty，run 透传段除外"},
		},
		"commands": []map[string]string{
			{"cmd": "add <path> [--name N] [--desc D] [--meta JSON]", "desc": "注册 exe；同名卡片无 exe 时附加；name 默认=文件名去 .exe 小写化"},
			{"cmd": "add-cmd --name N --program P --dir D --type gui|console [--arg A ...]", "desc": "注册/附加源码命令目标；同名卡片无源码目标时附加，否则创建仅源码卡片"},
			{"cmd": "edit <name> [--desc D] [--default exe|source] [--exe PATH] [--no-exe] [--source-program P] [--source-dir D] [--source-type gui|console] [--source-arg A ...] [--no-source] [--meta JSON]", "desc": "全量编辑卡片：exe、源码、默认目标、描述、meta"},
			{"cmd": "rm <name>", "desc": "删除注册（级联删其 launches）"},
			{"cmd": "set <name> --meta JSON", "desc": "整体替换 meta（传 {} 清空）"},
			{"cmd": "list [--status active|invalid] [--running]", "desc": "全部工具，名称升序；--running 只看后台活实例"},
			{"cmd": "info <name>", "desc": "详情 + 最近 10 条启动 + 累计耗时 + 后台运行状态 + 双目标状态"},
			{"cmd": "run [--exe|--source] <name> [args...]", "desc": "前台透传启动（要看输出、等结果的用它）；缺省走卡片默认目标；退出码=子进程码，未注册/失效=127"},
			{"cmd": "start [--console|--hidden|--detached] [--exe|--source] <name> [args...]", "desc": "启动并立即返回 pid；console=可见控制台，hidden=隐藏控制台（可随时显示/隐藏），detached=无控制台；缺省按目标类型自动选择"},
			{"cmd": "console <name> --show|--hide [--pid N]", "desc": "显示/隐藏运行中实例的控制台窗口，不重启进程；多实例缺省操作最新实例"},
			{"cmd": "stop <name>", "desc": "终止该工具全部后台活实例（taskkill 树杀）并闭环记录"},
			{"cmd": "cp <name> <dest_dir> [--force]", "desc": "复制已注册 exe 到目标目录（仅 exe 目标；目录须已存在，目标同名文件需 --force 覆盖）"},
			{"cmd": "serve [--port 0] [--open]", "desc": "启动内置 WebUI 与 REST 服务（与 GUI 同一 Handler；默认动态端口）"},
			{"cmd": "where", "desc": "自省：通道 / 版本 / 库路径 / 工具数 / 两通道 GUI 运行状态"},
			{"cmd": "completion powershell [--install|--uninstall]", "desc": "PowerShell Tab 补全脚本；--install 写入 $PROFILE，--uninstall 移除"},
			{"cmd": "completion names", "desc": "全部工具名，每行一个（供补全脚本消费，raw 输出）"},
			{"cmd": "mcp", "desc": "启动 stdio MCP server（GUI/AI 客户端对话通道；不解析参数，日志走 stderr）"},
			{"cmd": "schema", "desc": "导出 MCP 工具定义（含 JSON Schema），与 tools/list 同源；离线检查用"},
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
	tool, err := mustService().Add(positional[0], *name, *desc, json.RawMessage(*meta))
	if err != nil {
		failService(err, false)
		return 1
	}
	Emit(tool)
	return 0
}

// stringList 可重复 flag，如 --arg run --arg zedhub-gui
type stringList []string

func (s *stringList) String() string { return strings.Join(*s, " ") }

func (s *stringList) Set(v string) error {
	*s = append(*s, v)
	return nil
}

// cmdAddCmd 注册/附加源码命令目标。
// 同名卡片没有源码目标时附加；没有同名卡片时创建仅源码卡片。
func cmdAddCmd(args []string) int {
	fs := newFlagSet("add-cmd")
	name := fs.String("name", "", "调用名；缺省取最后一个固定参数的文件名")
	program := fs.String("program", "", "源码程序（如 uv 或 uv.exe 绝对路径）")
	dir := fs.String("dir", "", "源码命令工作目录")
	typ := fs.String("type", "gui", "源码类型: gui | console")
	desc := fs.String("desc", "", "描述")
	meta := fs.String("meta", "", "扩展属性 JSON")
	var fixed []string
	fs.Var((*stringList)(&fixed), "arg", "固定前缀参数，可重复")
	flags, positional := splitFlags(args, map[string]bool{
		"name": true, "program": true, "dir": true, "type": true,
		"desc": true, "meta": true, "arg": true,
	})
	if !parseFlags(fs, flags) {
		return 0
	}
	if len(positional) > 0 {
		Fail("bad_args", "add-cmd 不接受位置参数；固定参数请用 --arg，例如 --arg run --arg zedhub-gui")
		return 1
	}
	tool, err := mustService().AddCommand(service.CommandInput{
		Name:    *name,
		Program: *program,
		Workdir: *dir,
		Args:    fixed,
		Type:    *typ,
		Desc:    *desc,
		Meta:    json.RawMessage(*meta),
	})
	if err != nil {
		failService(err, false)
		return 1
	}
	Emit(tool)
	return 0
}

// cmdEdit 全量编辑卡片：描述、默认目标、exe、源码、meta。
func cmdEdit(args []string) int {
	noExe, noSource := false, false
	rest := make([]string, 0, len(args))
	for _, a := range args {
		switch a {
		case "--no-exe":
			noExe = true
		case "--no-source":
			noSource = true
		default:
			rest = append(rest, a)
		}
	}

	fs := newFlagSet("edit")
	desc := fs.String("desc", "", "描述")
	def := fs.String("default", "", "默认目标: exe | source")
	exe := fs.String("exe", "", "替换 exe 路径")
	srcProgram := fs.String("source-program", "", "替换源码程序")
	srcDir := fs.String("source-dir", "", "替换源码工作目录")
	srcType := fs.String("source-type", "", "替换源码类型: gui | console")
	var srcArgs []string
	fs.Var((*stringList)(&srcArgs), "source-arg", "替换源码固定参数，可重复")
	meta := fs.String("meta", "", "整体替换 meta JSON")
	flags, positional := splitFlags(rest, map[string]bool{
		"desc": true, "default": true, "exe": true,
		"source-program": true, "source-dir": true, "source-type": true,
		"source-arg": true, "meta": true,
	})
	if !parseFlags(fs, flags) {
		return 0
	}
	if len(positional) != 1 {
		Fail("bad_args", "用法: clictl edit <name> [--desc D] [--default exe|source] [--exe PATH] [--no-exe] [--source-program P] [--source-dir D] [--source-type gui|console] [--source-arg A ...] [--no-source] [--meta JSON]")
		return 1
	}

	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })

	info, err := mustService().Info(positional[0])
	if err != nil {
		failService(err, false)
		return 1
	}
	current := info.Tool

	description := current.Description
	if set["desc"] {
		description = *desc
	}
	defaultTarget := current.DefaultTarget
	if set["default"] {
		defaultTarget = *def
	}

	var exePath *string
	switch {
	case noExe:
	case set["exe"]:
		p := *exe
		exePath = &p
	case current.Exe != nil:
		p := current.Exe.Path
		exePath = &p
	}

	var source *service.CommandInput
	sourceTouched := set["source-program"] || set["source-dir"] || set["source-type"] || set["source-arg"]
	switch {
	case noSource:
	case sourceTouched:
		in := service.CommandInput{}
		if current.Source != nil {
			in.Program = current.Source.Program
			in.Workdir = current.Source.Workdir
			in.Type = current.Source.Type
			in.Args = append([]string{}, current.Source.Args...)
		}
		if set["source-program"] {
			in.Program = *srcProgram
		}
		if set["source-dir"] {
			in.Workdir = *srcDir
		}
		if set["source-type"] {
			in.Type = *srcType
		}
		if set["source-arg"] {
			in.Args = srcArgs
		}
		source = &in
	case current.Source != nil:
		source = &service.CommandInput{
			Program: current.Source.Program,
			Workdir: current.Source.Workdir,
			Args:    append([]string{}, current.Source.Args...),
			Type:    current.Source.Type,
		}
	}

	var metaRaw json.RawMessage
	if set["meta"] {
		metaRaw = json.RawMessage(*meta)
	} else {
		metaRaw = current.Meta
	}

	tool, err := mustService().Update(service.UpdateInput{
		Name:          positional[0],
		Description:   description,
		Meta:          metaRaw,
		DefaultTarget: defaultTarget,
		ExePath:       exePath,
		Source:        source,
	})
	if err != nil {
		failService(err, false)
		return 1
	}
	Emit(tool)
	return 0
}

// cmdConsole 显示/隐藏运行中实例的控制台窗口。
func cmdConsole(args []string) int {
	var visible *bool
	pid := 0
	var positional []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--show":
			v := true
			visible = &v
		case "--hide":
			v := false
			visible = &v
		case "--pid":
			if i+1 >= len(args) {
				Fail("bad_args", "--pid 缺少值")
				return 1
			}
			n, err := strconv.Atoi(args[i+1])
			if err != nil || n <= 0 {
				Fail("bad_args", "非法 PID: "+args[i+1])
				return 1
			}
			pid = n
			i++
		default:
			positional = append(positional, args[i])
		}
	}
	if len(positional) != 1 || visible == nil {
		Fail("bad_args", "用法: clictl console <name> --show|--hide [--pid N]")
		return 1
	}
	res, err := mustService().SetConsoleVisible(positional[0], pid, *visible)
	if err != nil {
		failService(err, false)
		return 1
	}
	Emit(res)
	return 0
}

func cmdRm(args []string) int {
	if len(args) != 1 {
		Fail("bad_args", "用法: clictl rm <name>")
		return 1
	}
	res, err := mustService().Remove(args[0])
	if err != nil {
		failService(err, false)
		return 1
	}
	Emit(res)
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
	tool, err := mustService().SetMeta(positional[0], json.RawMessage(*meta))
	if err != nil {
		failService(err, false)
		return 1
	}
	Emit(tool)
	return 0
}

func cmdList(args []string) int {
	fs := newFlagSet("list")
	status := fs.String("status", "", "按状态过滤: active | invalid")
	running := fs.Bool("running", false, "只列出有后台活实例的工具")
	flags, _ := splitFlags(args, map[string]bool{"status": true})
	if !parseFlags(fs, flags) {
		return 0
	}
	svc := mustService()
	var res any
	var err error
	if *running {
		if *status != "" {
			Fail("bad_args", "--running 与 --status 互斥")
			return 1
		}
		res, err = svc.ListRunning()
	} else {
		res, err = svc.ListTools(*status)
	}
	if err != nil {
		failService(err, false)
		return 1
	}
	Emit(res)
	return 0
}

func cmdInfo(args []string) int {
	if len(args) != 1 {
		Fail("bad_args", "用法: clictl info <name>")
		return 1
	}
	res, err := mustService().Info(args[0])
	if err != nil {
		failService(err, false)
		return 1
	}
	Emit(res)
	return 0
}

func cmdRun(args []string) int {
	target := ""
	for len(args) > 0 {
		switch args[0] {
		case "--exe":
			target = store.TargetExe
		case "--source":
			target = store.TargetSource
		default:
			goto parsed
		}
		args = args[1:]
	}
parsed:
	if len(args) < 1 {
		FailStderr("bad_args", "用法: clictl run [--exe|--source] <name> [args...]", 1)
		return 1
	}
	code, err := runner.Run(mustService().Store(), store.NormalizeName(args[0]), target, args[1:])
	if err != nil {
		var re *runner.RunError
		if errors.As(err, &re) {
			FailStderr(re.Code, re.Message, re.ExitCode, re.Suggestions...)
			return re.ExitCode
		}
		FailStderr("internal", err.Error(), 1)
		return 1
	}
	return code
}

func cmdStart(args []string) int {
	// 启动模式/目标只在前置段生效；工具名之后的同名参数属于子进程。
	mode, target := "", ""
loop:
	for len(args) > 0 {
		switch args[0] {
		case "--console":
			mode = runner.ModeConsole
		case "--hidden":
			mode = runner.ModeHidden
		case "--detached":
			mode = runner.ModeDetached
		case "--exe":
			target = store.TargetExe
		case "--source":
			target = store.TargetSource
		default:
			break loop
		}
		args = args[1:]
	}
	if len(args) < 1 {
		Fail("bad_args", "用法: clictl start [--console|--hidden|--detached] [--exe|--source] <name> [args...]")
		return 1
	}
	res, err := mustService().StartWithMode(args[0], target, args[1:], mode)
	if err != nil {
		failService(err, false)
		return 1
	}
	Emit(res)
	return 0
}

func cmdStop(args []string) int {
	if len(args) != 1 {
		Fail("bad_args", "用法: clictl stop <name>")
		return 1
	}
	res, err := mustService().Stop(args[0])
	if err != nil {
		failService(err, false)
		return 1
	}
	Emit(res)
	// 全杀成功 = 0；有杀后仍存活的实例 = 1
	if len(res.Failed) > 0 {
		return 1
	}
	return 0
}

// cmdCp 把已注册的 exe 复制到指定目录。--force 是布尔 flag，不走
// splitFlags（其约定所有 flag 带值），参照 --pretty 先例手动剥离，可位于任意位置。
func cmdCp(args []string) int {
	force := false
	rest := make([]string, 0, len(args))
	for _, a := range args {
		if a == "--force" {
			force = true
			continue
		}
		rest = append(rest, a)
	}
	if len(rest) != 2 {
		Fail("bad_args", "用法: clictl cp <name> <dest_dir> [--force]")
		return 1
	}
	res, err := mustService().Cp(rest[0], rest[1], force)
	if err != nil {
		failService(err, false)
		return 1
	}
	Emit(res)
	return 0
}

// failFromErr 已随服务层抽取移至 internal/service.wrapErr；
// CLI 层统一走 failService。

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

// isGlobalFlag 判断 a 是否 clictl 全局布尔 flag（可位于子命令前后，
// run/start 透传段除外）
func isGlobalFlag(a string) bool {
	return a == "--pretty" || a == "--ascii"
}

// setGlobalFlag 按名置位全局 flag（与 isGlobalFlag 配对使用）
func setGlobalFlag(a string) {
	switch a {
	case "--pretty":
		Pretty = true
	case "--ascii":
		Ascii = true
	}
}

// stripGlobalFlags 过滤掉 args 中的全局 flag 并置位对应开关
func stripGlobalFlags(args []string) []string {
	out := make([]string, 0, len(args))
	for _, a := range args {
		if isGlobalFlag(a) {
			setGlobalFlag(a)
			continue
		}
		out = append(out, a)
	}
	return out
}
