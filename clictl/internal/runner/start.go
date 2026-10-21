// start.go 后台分离启动：点火即走。
package runner

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"

	"clictl/internal/store"

	"golang.org/x/sys/windows"
)

// 启动模式：detached = 无控制台后台分离（GUI/托盘/服务类）；
// console = 独立控制台窗口（命令行工具，用户可见、可交互）。
const (
	ModeDetached = "detached"
	ModeConsole  = "console"
)

// StartResult start 命令的 JSON 输出形状
type StartResult struct {
	Name           string `json:"name"`
	Path           string `json:"path"`
	PID            int    `json:"pid"`
	StartedAt      string `json:"started_at"`
	AlreadyRunning bool   `json:"already_running"` // 启动前已有活实例（仅提示，不拦截）
	Mode           string `json:"mode"`            // detached / console
}

// Start 后台分离启动：DETACHED_PROCESS（无控制台，GUI/托盘不闪黑框）+
// CREATE_NEW_PROCESS_GROUP（独立进程组，与 clictl 生命周期彻底解耦）。
// 不接任何 stdio、不 Wait、退出码无从谈起——记录只落 started_at + pid，
// 永不闭环（duration_ms/exit_code 恒 NULL），是否仍在运行由探活判定。
// 与 run 对比：要看输出、要等结果的用 run；点火就走的用 start。
func Start(st *store.Store, name string, args []string) (StartResult, *RunError) {
	tool, rerr := lookup(st, name)
	if rerr != nil {
		return StartResult{}, rerr
	}

	// 已有活实例提示（多实例自由，不拦截）
	unfinished, err := st.UnfinishedStarts(tool.ID)
	if err != nil {
		return StartResult{}, &RunError{Code: "db_error", Message: "查询启动记录失败: " + err.Error(), ExitCode: 1}
	}
	alreadyRunning := len(FilterAlive(unfinished, tool.Path)) > 0

	start := time.Now()
	launchID, err := st.InsertLaunch(tool.ID, start)
	if err != nil {
		return StartResult{}, &RunError{Code: "db_error", Message: "写入启动记录失败: " + err.Error(), ExitCode: 1}
	}

	cmd := exec.Command(tool.Path, args...)
	cmd.SysProcAttr = &windows.SysProcAttr{
		CreationFlags: windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP,
	}
	if err := cmd.Start(); err != nil {
		// 启动失败也留痕：回写 127（无法执行）
		_ = st.FinishLaunch(launchID, time.Since(start).Milliseconds(), 127)
		return StartResult{}, &RunError{Code: "start_failed", Message: "启动失败: " + err.Error(), ExitCode: 127}
	}
	pid := cmd.Process.Pid
	if err := st.SetLaunchPID(launchID, pid); err != nil {
		return StartResult{}, &RunError{Code: "db_error", Message: "回写 PID 失败: " + err.Error(), ExitCode: 1}
	}

	return StartResult{
		Name:           tool.Name,
		Path:           tool.Path,
		PID:            pid,
		StartedAt:      start.UTC().Format(store.TimeFmt),
		AlreadyRunning: alreadyRunning,
		Mode:           ModeDetached,
	}, nil
}

// StartConsole 为命令行工具开独立控制台窗口启动（CREATE_NEW_CONSOLE），
// 后台等待退出并闭环记录。
//
// 与 Start 的差异只在 CreationFlags：console 程序在 DETACHED 下没有控制台，
// 输出与交互全部不可见；CREATE_NEW_CONSOLE 让系统分配新窗口，进程本身仍是
// 分离的（不挂 clictl 的生命周期）。窗口关闭即进程结束，Wait 返回后回写
// duration/exit_code；pid 为真实 exe，探活与 stop 树杀语义与后台启动一致。
func StartConsole(st *store.Store, name string, args []string) (StartResult, *RunError) {
	tool, rerr := lookup(st, name)
	if rerr != nil {
		return StartResult{}, rerr
	}

	unfinished, err := st.UnfinishedStarts(tool.ID)
	if err != nil {
		return StartResult{}, &RunError{Code: "db_error", Message: "查询启动记录失败: " + err.Error(), ExitCode: 1}
	}
	alreadyRunning := len(FilterAlive(unfinished, tool.Path)) > 0

	start := time.Now()
	launchID, err := st.InsertLaunch(tool.ID, start)
	if err != nil {
		return StartResult{}, &RunError{Code: "db_error", Message: "写入启动记录失败: " + err.Error(), ExitCode: 1}
	}

	cmd := exec.Command(tool.Path, args...)
	cmd.SysProcAttr = &windows.SysProcAttr{
		CreationFlags: windows.CREATE_NEW_CONSOLE,
	}
	if err := cmd.Start(); err != nil {
		_ = st.FinishLaunch(launchID, time.Since(start).Milliseconds(), 127)
		return StartResult{}, &RunError{Code: "start_failed", Message: "启动失败: " + err.Error(), ExitCode: 127}
	}
	pid := cmd.Process.Pid
	if err := st.SetLaunchPID(launchID, pid); err != nil {
		return StartResult{}, &RunError{Code: "db_error", Message: "回写 PID 失败: " + err.Error(), ExitCode: 1}
	}

	go func() {
		_ = cmd.Wait()
		exitCode := cmd.ProcessState.ExitCode()
		if exitCode < 0 {
			exitCode = 1 // 被强制结束等无法取码的情况
		}
		_ = st.FinishLaunch(launchID, time.Since(start).Milliseconds(), exitCode)
	}()

	return StartResult{
		Name:           tool.Name,
		Path:           tool.Path,
		PID:            pid,
		StartedAt:      start.UTC().Format(store.TimeFmt),
		AlreadyRunning: alreadyRunning,
		Mode:           ModeConsole,
	}, nil
}

// lookup run/start/stop 共用的前置校验：查注册 + 现场校验文件有效性 + 未注册相似名建议
func lookup(st *store.Store, name string) (store.Tool, *RunError) {
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
	if fi, statErr := os.Stat(tool.Path); statErr != nil || fi.IsDir() {
		_ = st.RefreshStatus(tool.ID, false)
		return store.Tool{}, &RunError{Code: "invalid", Message: "文件已失效: " + tool.Path, ExitCode: 127}
	}
	return tool, nil
}
