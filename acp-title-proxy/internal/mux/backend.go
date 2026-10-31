package mux

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"sync"
)

// Handler 接收后端进程的每一行输出与退出事件。
type Handler interface {
	OnLine(line []byte)
	OnExit(err error)
}

// Conn 抽象后端进程，便于测试时注入 mock。
type Conn interface {
	WriteLine(line []byte) error
	Close() error
	PID() int
}

type Backend struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	stderr io.ReadCloser

	writeMu   sync.Mutex
	closeOnce sync.Once
}

// StartBackend 启动 agy_acp_server 并接管其 stdio。
func StartBackend(exe string, args []string, env []string, h Handler) (*Backend, error) {
	cmd := exec.Command(exe, args...)
	if len(env) > 0 {
		cmd.Env = env
	} else {
		cmd.Env = os.Environ()
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		_ = stderr.Close()
		return nil, fmt.Errorf("start backend: %w", err)
	}

	b := &Backend{
		cmd:    cmd,
		stdin:  stdin,
		stdout: bufio.NewReaderSize(stdout, 4*1024*1024),
		stderr: stderr,
	}

	go b.readLoop(h)
	go func() {
		_, _ = io.Copy(os.Stderr, stderr)
	}()
	go func() {
		err := cmd.Wait()
		h.OnExit(err)
	}()

	return b, nil
}

func (b *Backend) readLoop(h Handler) {
	for {
		line, err := b.stdout.ReadBytes('\n')
		line = bytes.TrimRight(line, "\r\n")
		if len(line) > 0 {
			h.OnLine(line)
		}
		if err != nil {
			return
		}
	}
}

func (b *Backend) WriteLine(line []byte) error {
	b.writeMu.Lock()
	defer b.writeMu.Unlock()
	if b.stdin == nil {
		return fmt.Errorf("backend stdin closed")
	}
	if _, err := b.stdin.Write(append(line, '\n')); err != nil {
		return err
	}
	return nil
}

func (b *Backend) Close() error {
	b.closeOnce.Do(func() {
		if b.stdin != nil {
			_ = b.stdin.Close()
		}
		if b.cmd != nil && b.cmd.Process != nil {
			_ = KillTree(b.cmd.Process.Pid)
		}
		if b.stderr != nil {
			_ = b.stderr.Close()
		}
	})
	return nil
}

func (b *Backend) PID() int {
	if b.cmd != nil && b.cmd.Process != nil {
		return b.cmd.Process.Pid
	}
	return 0
}

// KillTree 终止进程及其全部子进程（Windows 上必须连坐）。
func KillTree(pid int) error {
	if pid <= 0 {
		return nil
	}
	if runtime.GOOS == "windows" {
		return exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(pid)).Run()
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return p.Kill()
}
