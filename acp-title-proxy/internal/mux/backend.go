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

	"language_projects/go_projects/acp-title-proxy/internal/procguard"
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

	// guard 是 Windows Job Object 守护句柄：Close 或宿主进程退出时，
	// 内核会连带终止后端及其 localharness 子进程，避免产生孤儿泄漏。
	guard *procguard.Guard

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

	// 把后端进程绑定到 Job Object：宿主（daemon/客户端）一旦退出，
	// agy_acp_server 与它派生的 localharness_external 会被内核连带终止。
	if guard, gerr := procguard.Attach(cmd.Process.Pid); gerr != nil {
		fmt.Fprintf(os.Stderr, "[acp-title-proxy] job object attach failed (backend pid %d): %v\n", cmd.Process.Pid, gerr)
	} else {
		b.guard = guard
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
		// Job 关闭会连带回收后端整棵进程树（含 localharness），
		// 即使 taskkill 未能覆盖的子进程也会被内核终止。
		if b.guard != nil {
			_ = b.guard.Close()
			b.guard = nil
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
