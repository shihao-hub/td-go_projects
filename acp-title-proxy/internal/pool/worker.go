package pool

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"time"
)

// KillProcessTree terminates the process and all of its child processes on Windows.
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

	// Discard stderr in background
	go func() {
		_, _ = io.Copy(io.Discard, stderr)
	}()

	waitErr := make(chan error, 1)
	go func() {
		waitErr <- cmd.Wait()
	}()

	return &Worker{
		ExePath:      exePath,
		Cmd:          cmd,
		Stdin:        stdin,
		Stdout:       stdout,
		StdoutReader: bufio.NewReaderSize(stdout, 1024*1024),
		Stderr:       stderr,
		CreatedAt:    time.Now(),
		WaitErr:      waitErr,
	}, nil
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
	})
	return nil
}
