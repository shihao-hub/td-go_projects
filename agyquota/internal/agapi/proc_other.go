//go:build !windows

package agapi

import (
	"bytes"
	"fmt"
	"io"
	"os/exec"
)

// agyProcess 非 Windows 平台用 os/exec 简单封装，语义与 Windows 版对齐
type agyProcess struct {
	cmd    *exec.Cmd
	stdout *bytes.Buffer
	stderr *bytes.Buffer
}

// startAgyProcess 非 Windows 平台无默认终端委托问题，直接常规启动
func startAgyProcess(exePath string) (*agyProcess, error) {
	cmd := exec.Command(exePath, "-p", "/usage", "--output-format", "json")
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("启动 agy 进程失败: %w", err)
	}
	return &agyProcess{cmd: cmd, stdout: stdout, stderr: stderr}, nil
}

// Stdout 返回 agy 原始标准输出。
func (p *agyProcess) Stdout() io.Reader { return p.stdout }

// Stderr 返回 agy 原始标准错误。
func (p *agyProcess) Stderr() io.Reader { return p.stderr }

// Wait 阻塞等待 agy 退出并返回退出码。
func (p *agyProcess) Wait() (uint32, error) {
	if err := p.cmd.Wait(); err != nil {
		return 1, err
	}
	return 0, nil
}

// shutdown 终止 agy 进程；幂等。
func (p *agyProcess) shutdown() {
	if p.cmd != nil && p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
}
