package mcp

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"

	"clictl/internal/service"
)

// run 工具的非交互执行核心（跨平台；进程树管理见 exec_windows.go / exec_other.go）。

const (
	// maxStreamBytes 单流（stdout/stderr）保留上限 1 MiB：超出继续排空管道
	// （防子进程写满 pipe 阻塞死锁），仅标记截断
	maxStreamBytes = 1 << 20

	// defaultRunTimeoutMs run 工具缺省超时
	defaultRunTimeoutMs = 60000
)

// runOut clictl.run 的输出形状。ExitCode 在超时/取消（进程被强杀）时为
// 约定值 1（TerminateProcess 惯例，对齐 stop 命令）；正常结束为子进程退出码。
type runOut struct {
	Name            string `json:"name"`
	Path            string `json:"path"`
	ExitCode        int    `json:"exit_code"`
	DurationMs      int64  `json:"duration_ms"`
	Stdout          string `json:"stdout"`
	Stderr          string `json:"stderr"`
	StdoutTruncated bool   `json:"stdout_truncated"`
	StderrTruncated bool   `json:"stderr_truncated"`
	TimedOut        bool   `json:"timed_out"`
	Cancelled       bool   `json:"cancelled"`
}

// failureSummary IsError 时的人类可读摘要
func (o runOut) failureSummary() string {
	switch {
	case o.TimedOut:
		return fmt.Sprintf("执行超时，已终止整棵进程树（exit_code=%d，duration_ms=%d）", o.ExitCode, o.DurationMs)
	case o.Cancelled:
		return fmt.Sprintf("执行被取消，已终止整棵进程树（exit_code=%d，duration_ms=%d）", o.ExitCode, o.DurationMs)
	default:
		return fmt.Sprintf("进程以退出码 %d 结束", o.ExitCode)
	}
}

// limitedBuffer 读到上限后继续消费写入（返回成功）但不再保留，防 pipe
// 写满阻塞子进程；truncated 标记是否丢弃过内容。
// 仅被 exec 包的单个 copy goroutine 写入，无需加锁。
type limitedBuffer struct {
	buf       bytes.Buffer
	truncated bool
}

func (lb *limitedBuffer) Write(p []byte) (int, error) {
	if remain := maxStreamBytes - lb.buf.Len(); remain > 0 {
		if len(p) <= remain {
			lb.buf.Write(p)
			return len(p), nil
		}
		lb.buf.Write(p[:remain])
	}
	lb.truncated = true
	return len(p), nil // 声明全部消费，调用方（exec copy）继续排空
}

// execTool 非交互执行：stdin 关闭、参数数组直传、收集输出与退出码。
// 取消（ctx）/超时 → 杀整棵进程树；启动记账闭环与 CLI run 一致。
func execTool(ctx context.Context, svc *service.Service, name string, args []string, timeoutMs int64) (runOut, error) {
	tool, err := svc.PreflightRun(name)
	if err != nil {
		return runOut{}, err
	}
	out := runOut{Name: tool.Name, Path: tool.Path}

	start := time.Now()
	launchID, err := svc.Store().InsertLaunch(tool.ID, start)
	if err != nil {
		return out, &service.Error{Code: "db_error", Message: "写入启动记录失败: " + err.Error()}
	}
	finish := func(code int) {
		_ = svc.Store().FinishLaunch(launchID, time.Since(start).Milliseconds(), code)
	}

	if timeoutMs <= 0 {
		timeoutMs = defaultRunTimeoutMs
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()

	tree, err := newProcTree()
	if err != nil {
		finish(127)
		return out, &service.Error{Code: "start_failed", Message: "创建进程树失败: " + err.Error()}
	}
	defer tree.close() // server 崩溃时由 OS 兜底回收整树（KILL_ON_JOB_CLOSE）

	cmd := exec.Command(tool.Path, args...)
	cmd.Stdin = nil // 非交互：子进程 stdin 连接 os.DevNull
	var stdout, stderr limitedBuffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	tree.setup(cmd)

	if err := cmd.Start(); err != nil {
		finish(127)
		return out, &service.Error{Code: "start_failed", Message: "启动失败: " + err.Error()}
	}
	if err := tree.attach(cmd); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		finish(127)
		return out, &service.Error{Code: "start_failed", Message: "进程树接管失败: " + err.Error()}
	}

	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait() }()

	select {
	case <-waitDone:
		// 正常结束
	case <-ctx.Done():
		tree.kill()
		out.TimedOut = ctx.Err() == context.DeadlineExceeded
		out.Cancelled = !out.TimedOut
		<-waitDone // 等收尾（管道 copy 完成后 Wait 才返回，输出不丢）
	}

	out.DurationMs = time.Since(start).Milliseconds()
	out.Stdout = stdout.buf.String()
	out.Stderr = stderr.buf.String()
	out.StdoutTruncated = stdout.truncated
	out.StderrTruncated = stderr.truncated

	exitCode := 1 // 强杀/信号杀死/无法取码的约定值
	if cmd.ProcessState != nil {
		if c := cmd.ProcessState.ExitCode(); c >= 0 {
			exitCode = c
		}
	}
	out.ExitCode = exitCode
	finish(exitCode)
	return out, nil
}
