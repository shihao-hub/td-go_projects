// Package runner 负责透传启动与启动记账。
package runner

import (
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"time"

	"clictl/internal/store"
)

// RunError run/start 前置阶段失败（未注册/目标失效/启动失败），
// 由调用方以 stderr JSON 输出并按 ExitCode 退出。
// Suggestions 为未注册时的相似名提示（可选，空则不输出该字段）
type RunError struct {
	Code        string
	Message     string
	ExitCode    int
	Suggestions []string
}

func (e *RunError) Error() string { return e.Message }

// LaunchSpec 一次启动的完整目标（exe 或源码命令）
type LaunchSpec struct {
	Tool    store.Tool
	Target  string
	Program string
	Args    []string
	Dir     string // 源码命令的工作目录；exe 为空
	Console bool   // 该目标的默认控制台属性
}

// BuildSpec 按目标解析启动命令。target 为空时用卡片的 default_target。
func BuildSpec(tool store.Tool, target string, extraArgs []string) (LaunchSpec, *RunError) {
	if target == "" {
		target = tool.DefaultTarget
	}
	switch target {
	case store.TargetExe:
		if tool.Exe == nil {
			return LaunchSpec{}, &RunError{Code: "no_exe_target", Message: "该工具没有 exe 目标", ExitCode: 1}
		}
		if tool.Exe.Status != store.StatusActive {
			return LaunchSpec{}, &RunError{Code: "invalid", Message: "exe 文件已失效: " + tool.Exe.Path, ExitCode: 127}
		}
		return LaunchSpec{
			Tool:    tool,
			Target:  store.TargetExe,
			Program: tool.Exe.Path,
			Args:    extraArgs,
			Console: IsConsoleExe(tool.Exe.Path),
		}, nil
	case store.TargetSource:
		if tool.Source == nil {
			return LaunchSpec{}, &RunError{Code: "no_source_target", Message: "该工具没有源码命令目标", ExitCode: 1}
		}
		if tool.Source.Status != store.StatusActive {
			return LaunchSpec{}, &RunError{Code: "invalid", Message: "源码命令失效（程序或工作目录不存在）: " + tool.Source.Workdir, ExitCode: 127}
		}
		args := make([]string, 0, len(tool.Source.Args)+len(extraArgs))
		args = append(args, tool.Source.Args...)
		args = append(args, extraArgs...)
		return LaunchSpec{
			Tool:    tool,
			Target:  store.TargetSource,
			Program: tool.Source.Program,
			Args:    args,
			Dir:     tool.Source.Workdir,
			Console: tool.Source.Type == store.SourceTypeConsole,
		}, nil
	default:
		return LaunchSpec{}, &RunError{Code: "bad_args", Message: "target 仅支持 exe | source", ExitCode: 1}
	}
}

// Run 透传启动：stdin/stdout/stderr 与退出码全部直通，不经任何 shell 包裹。
// 子进程启动成功后总是返回其退出码；前置失败返回 *RunError。
func Run(st *store.Store, name, target string, args []string) (int, error) {
	tool, rerr := lookupTool(st, name)
	if rerr != nil {
		return 0, rerr
	}
	spec, rerr := BuildSpec(tool, target, args)
	if rerr != nil {
		return 0, rerr
	}
	return runSpec(st, spec)
}

func runSpec(st *store.Store, spec LaunchSpec) (int, error) {
	start := time.Now()
	launchID, err := st.InsertLaunch(spec.Tool.ID, start, spec.Target, store.ModeRun)
	if err != nil {
		return 0, &RunError{Code: "db_error", Message: "写入启动记录失败: " + err.Error(), ExitCode: 1}
	}

	// 忽略父进程 Ctrl+C：同控制台的子进程照常收到并退出，
	// 父进程 Wait 返回后回写 duration/exit_code 再退，记录不丢
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	defer signal.Stop(sigCh)
	go func() {
		for range sigCh { // 吞掉信号
		}
	}()

	cmd := exec.Command(spec.Program, spec.Args...)
	cmd.Dir = spec.Dir
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		// 启动失败也留痕：回写耗时与 127（无法执行）
		_ = st.FinishLaunch(launchID, time.Since(start).Milliseconds(), 127)
		return 0, &RunError{Code: "start_failed", Message: "启动失败: " + err.Error(), ExitCode: 127}
	}

	_ = cmd.Wait()
	exitCode := cmd.ProcessState.ExitCode()
	if exitCode < 0 {
		exitCode = 1 // 被信号杀死等无法取码的情况
	}
	_ = st.FinishLaunch(launchID, time.Since(start).Milliseconds(), exitCode)
	return exitCode, nil
}

// lookupTool run/start/stop 共用的前置校验：查注册 + 未注册相似名建议。
// 具体目标是否有效由 BuildSpec 判定（一张卡片可能 exe 失效但源码仍可用）。
func lookupTool(st *store.Store, name string) (store.Tool, *RunError) {
	tool, err := st.GetTool(name)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			msg := "未注册的工具: " + name
			var suggestions []string
			if tools, lerr := st.ListTools(""); lerr == nil {
				names := make([]string, 0, len(tools))
				for _, t := range tools {
					names = append(names, t.Name)
				}
				if s := suggest(name, names, 3); len(s) > 0 {
					suggestions = s
					msg += "，是否想找: " + strings.Join(s, " / ")
				}
			}
			return store.Tool{}, &RunError{Code: "not_found", Message: msg, ExitCode: 127, Suggestions: suggestions}
		}
		return store.Tool{}, &RunError{Code: "db_error", Message: err.Error(), ExitCode: 1}
	}
	return tool, nil
}
