package pool

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"language_projects/go_projects/acp-title-proxy/internal/procguard"
)

// KillProcessTree 在 Windows 上终止该进程及其全部子进程。
func KillProcessTree(pid int) error {
	if pid <= 0 {
		return nil
	}
	if runtime.GOOS == "windows" {
		cmd := exec.Command("taskkill", "/F", "/T", "/PID", fmt.Sprintf("%d", pid))
		return cmd.Run()
	}
	p, err := os.FindProcess(pid)
	if err == nil {
		return p.Kill()
	}
	return err
}

type Worker struct {
	ExePath      string
	Cmd          *exec.Cmd
	Stdin        io.WriteCloser
	Stdout       io.ReadCloser
	StdoutReader *bufio.Reader
	Stderr       io.ReadCloser
	CreatedAt    time.Time
	WaitErr      chan error
	closeOnce    sync.Once

	// guard 是 Windows Job Object 守护句柄：daemon 退出时内核连带终止 worker 与 harness。
	guard *procguard.Guard

	// lastActive 记录最近一次 ACP 流量时间（UnixNano）。
	// reaper 用它判断该 worker 是否真的空闲：只看 CPU 会在等模型响应、
	// 等用户批准工具调用时误判为空闲，从而杀掉正在干活的 harness。
	lastActive atomic.Int64
}

// Touch 标记该 worker 刚有 ACP 流量往来。
func (w *Worker) Touch() {
	w.lastActive.Store(time.Now().UnixNano())
}

// LastActive 返回最近一次流量时间；从未有过流量时回落到创建时间。
func (w *Worker) LastActive() time.Time {
	ns := w.lastActive.Load()
	if ns == 0 {
		return w.CreatedAt
	}
	return time.Unix(0, ns)
}

func StartWorker(exePath string, args []string, env []string) (*Worker, error) {
	cmd := exec.Command(exePath, args...)
	if len(env) > 0 {
		cmd.Env = env
	} else {
		cmd.Env = os.Environ()
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe error: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("stdout pipe error: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, fmt.Errorf("stderr pipe error: %w", err)
	}

	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		_ = stderr.Close()
		return nil, fmt.Errorf("start worker error: %w", err)
	}

	// 后台丢弃 stderr
	go func() {
		_, _ = io.Copy(io.Discard, stderr)
	}()

	waitErr := make(chan error, 1)
	go func() {
		waitErr <- cmd.Wait()
	}()

	w := &Worker{
		ExePath:      exePath,
		Cmd:          cmd,
		Stdin:        stdin,
		Stdout:       stdout,
		StdoutReader: bufio.NewReaderSize(stdout, 1024*1024),
		Stderr:       stderr,
		CreatedAt:    time.Now(),
		WaitErr:      waitErr,
	}

	// 预热池 worker 同样绑定 Job：daemon 退出时连带回收 worker 与 harness。
	if guard, gerr := procguard.Attach(cmd.Process.Pid); gerr != nil {
		fmt.Fprintf(os.Stderr, "[acp-title-proxy] job object attach failed (worker pid %d): %v\n", cmd.Process.Pid, gerr)
	} else {
		w.guard = guard
	}

	return w, nil
}

func (w *Worker) Close() error {
	w.closeOnce.Do(func() {
		if w.Stdin != nil {
			_ = w.Stdin.Close()
		}
		if w.Stdout != nil {
			_ = w.Stdout.Close()
		}
		if w.Stderr != nil {
			_ = w.Stderr.Close()
		}
		if w.Cmd != nil && w.Cmd.Process != nil {
			pid := w.Cmd.Process.Pid
			_ = KillProcessTree(pid)
			_ = w.Cmd.Process.Kill()
		}
		if w.guard != nil {
			_ = w.guard.Close()
			w.guard = nil
		}
	})
	return nil
}
