package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"
)

const createNoWindow = 0x08000000

type appOptions struct {
	Dir      string
	Host     string
	Port     int
	Generate bool
}

type proc struct {
	cmd  *exec.Cmd
	done chan error
}

type app struct {
	opt      appOptions
	zreadCmd string
	logPath  string

	mu     sync.Mutex
	cur    *proc
	events chan struct{}
}

func newApp(opt appOptions) (*app, error) {
	zreadCmd, err := exec.LookPath("zread")
	if err != nil {
		return nil, errors.New("未找到 zread 命令，请先执行 npm install -g zread 安装")
	}
	a := &app{
		opt:      opt,
		zreadCmd: zreadCmd,
		logPath:  filepath.Join(os.TempDir(), "zread-tray.log"),
		events:   make(chan struct{}, 1),
	}
	if err := a.Start(); err != nil {
		return nil, err
	}
	return a, nil
}

func (a *app) args() []string {
	args := []string{"browse"}
	if a.opt.Host != "" {
		args = append(args, "--host", a.opt.Host)
	}
	if a.opt.Port > 0 {
		args = append(args, "--port", strconv.Itoa(a.opt.Port))
	}
	if a.opt.Generate {
		args = append(args, "--generate")
	}
	return args
}

func (a *app) Start() error {
	logFile, err := os.OpenFile(a.logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("打开日志文件失败: %w", err)
	}

	cmd := exec.Command("cmd.exe", append([]string{"/c", a.zreadCmd}, a.args()...)...)
	cmd.Dir = a.opt.Dir
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	cmd.Stdout, cmd.Stderr = logFile, logFile

	fmt.Fprintf(logFile, "\n===== %s 启动 zread %s (dir=%s) =====\n", time.Now().Format("2006-01-02 15:04:05"), cmd.Args[3], a.opt.Dir)
	if err := cmd.Start(); err != nil {
		logFile.Close()
		return fmt.Errorf("启动 zread browse 失败: %w", err)
	}

	p := &proc{cmd: cmd, done: make(chan error, 1)}
	go func() {
		p.done <- cmd.Wait()
		logFile.Close()
		a.notify()
	}()

	a.mu.Lock()
	a.cur = p
	a.mu.Unlock()
	a.notify()
	return nil
}

func (a *app) Stop() {
	a.mu.Lock()
	p := a.cur
	a.cur = nil
	a.mu.Unlock()
	if p == nil || p.cmd.Process == nil {
		return
	}
	if err := killTree(p.cmd.Process.Pid); err != nil {
		_ = p.cmd.Process.Kill()
	}
	select {
	case <-p.done:
	case <-time.After(3 * time.Second):
	}
	a.notify()
}

func (a *app) Dir() string {
	return a.opt.Dir
}

func (a *app) Restart() error {
	a.Stop()
	return a.Start()
}

func (a *app) SwitchDir(dir string) error {
	a.Stop()
	a.opt.Dir = dir
	return a.Start()
}

func (a *app) Alive() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cur == nil {
		return false
	}
	select {
	case <-a.cur.done:
		return false
	default:
		return true
	}
}

func (a *app) Wait() {
	a.mu.Lock()
	p := a.cur
	a.mu.Unlock()
	if p != nil {
		<-p.done
	}
}

func (a *app) Events() <-chan struct{} {
	return a.events
}

func (a *app) notify() {
	select {
	case a.events <- struct{}{}:
	default:
	}
}

func killTree(pid int) error {
	cmd := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(pid))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	_, err := cmd.Output()
	return err
}
