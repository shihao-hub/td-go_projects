//go:build windows

package cli

import (
	"os"
	"os/exec"
	"syscall"
)

const createNewConsole = 0x00000010

// runForeground 前台透传启动：三个标准流全部继承给子进程，返回其退出码
func runForeground(path string, args []string) (int, error) {
	cmd := exec.Command(path, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode(), nil // 非零退出是正常结果，不是启动错误
		}
		return 0, err
	}
	return 0, nil
}

// openInExplorer 资源管理器中定位文件；explorer 立即返回，不等它退出
func openInExplorer(path string) error {
	cmd := exec.Command("explorer.exe", "/select,"+path)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewConsole}
	return cmd.Start()
}

// spawnShell 在 dir 下开新 PowerShell 窗口（新控制台，CLI 立即返回）
func spawnShell(dir string) error {
	cmd := exec.Command("powershell.exe", "-NoExit", "-WorkingDirectory", dir)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewConsole}
	if err := cmd.Start(); err != nil {
		return err
	}
	_ = cmd.Process.Release() // 不 Wait：窗口独立存活
	return nil
}
