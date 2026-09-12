// Package runner 负责透传启动与启动记账。
package runner

import (
	"os"
	"os/exec"
	"os/signal"
	"time"

	"clictl/internal/store"
)

// RunError run 前置阶段失败（未注册/文件失效/启动失败），
// 由调用方以 stderr JSON 输出并按 ExitCode 退出。
// Suggestions 为未注册时的相似名提示（可选，空则不输出该字段）
type RunError struct {
	Code        string
	Message     string
	ExitCode    int
	Suggestions []string
}

func (e *RunError) Error() string { return e.Message }

// Run 透传启动：stdin/stdout/stderr 与退出码全部直通，不经任何 shell 包裹。
// 子进程启动成功后总是返回其退出码；前置失败返回 *RunError。
func Run(st *store.Store, name string, args []string) (int, error) {
	tool, rerr := lookup(st, name)
	if rerr != nil {
		return 0, rerr
	}

	start := time.Now()
	launchID, err := st.InsertLaunch(tool.ID, start)
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

	cmd := exec.Command(tool.Path, args...)
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
