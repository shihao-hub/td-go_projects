package pool

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"
)

type Worker struct {
	Cmd       *exec.Cmd
	Stdin     io.WriteCloser
	Stdout    io.ReadCloser
	Stderr    io.ReadCloser
	CreatedAt time.Time
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

	return &Worker{
		Cmd:       cmd,
		Stdin:     stdin,
		Stdout:    stdout,
		Stderr:    stderr,
		CreatedAt: time.Now(),
	}, nil
}

func (w *Worker) Close() error {
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
		_ = w.Cmd.Process.Kill()
		_ = w.Cmd.Wait()
	}
	return nil
}
