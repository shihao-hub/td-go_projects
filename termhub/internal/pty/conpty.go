//go:build windows

// conpty.go：基于 UserExistsError/conpty 的真 PTY 工厂实现。
package pty

import (
	"fmt"

	"github.com/UserExistsError/conpty"
)

// ConPtyFactory 真 ConPTY 工厂。
type ConPtyFactory struct{}

// Start 启动一个带伪控制台的进程。
func (ConPtyFactory) Start(opts Options) (Term, error) {
	if !conpty.IsConPtyAvailable() {
		return nil, fmt.Errorf("当前 Windows 版本不支持 ConPTY")
	}
	cols, rows := opts.Cols, opts.Rows
	if cols <= 0 {
		cols = 120
	}
	if rows <= 0 {
		rows = 30
	}
	args := []conpty.ConPtyOption{conpty.ConPtyDimensions(cols, rows)}
	if opts.WorkDir != "" {
		args = append(args, conpty.ConPtyWorkDir(opts.WorkDir))
	}
	if len(opts.Env) > 0 {
		args = append(args, conpty.ConPtyEnv(opts.Env))
	}
	c, err := conpty.Start(opts.CommandLine, args...)
	if err != nil {
		return nil, fmt.Errorf("启动终端进程失败: %w", err)
	}
	return &conPtyTerm{c: c}, nil
}

// conPtyTerm 适配 *conpty.ConPty 到 Term 接口。
type conPtyTerm struct {
	c *conpty.ConPty
}

func (t *conPtyTerm) Read(p []byte) (int, error)  { return t.c.Read(p) }
func (t *conPtyTerm) Write(p []byte) (int, error) { return t.c.Write(p) }
func (t *conPtyTerm) Resize(cols, rows int) error { return t.c.Resize(cols, rows) }
func (t *conPtyTerm) Pid() int                    { return t.c.Pid() }

// Close 关闭 PTY；ConPTY 关闭会连带终止附着进程。
func (t *conPtyTerm) Close() error { return t.c.Close() }
