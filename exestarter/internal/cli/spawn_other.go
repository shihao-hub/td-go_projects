//go:build !windows

package cli

import (
	"os"
	"os/exec"
	"path/filepath"
)

// runForeground 非 Windows：前台透传启动，返回子进程退出码
func runForeground(path string, args []string) (int, error) {
	cmd := exec.Command(path, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode(), nil
		}
		return 0, err
	}
	return 0, nil
}

// openInExplorer 非 Windows：xdg-open 打开所在目录（无"定位文件"语义）
func openInExplorer(path string) error {
	dir := filepath.Dir(path)
	return exec.Command("xdg-open", dir).Start()
}

// spawnShell 非 Windows：用 $SHELL 在目录下开终端（尽力而为）
func spawnShell(dir string) error {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	cmd := exec.Command(shell, "-c", "cd \"$1\"; exec \"$SHELL\"", "sh", dir)
	return cmd.Start()
}
