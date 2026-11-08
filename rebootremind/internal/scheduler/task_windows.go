//go:build windows

// Package scheduler 封装 Windows 计划任务的安装、卸载与查询。
package scheduler

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

// DefaultTaskName 是计划任务的默认名称。
const DefaultTaskName = "rebootremind_check"

// ErrUnstableExecutable 表示当前进程来自 go run 的临时构建，不能注册计划任务。
var ErrUnstableExecutable = errors.New("当前 exe 位于临时构建目录，无法注册稳定的计划任务；请先构建 bin/rebootremind-dev.exe")

var runCommand = func(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: windows.CREATE_NO_WINDOW,
	}
	output, err := cmd.CombinedOutput()
	return decodeOEM(output), err
}

// decodeOEM 把控制台程序输出的 OEM 代码页字节（中文系统为 GBK）转为 UTF-8。
func decodeOEM(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	const cpOEM = 1
	needed, err := windows.MultiByteToWideChar(cpOEM, 0, &data[0], int32(len(data)), nil, 0)
	if err != nil || needed <= 0 {
		return string(data)
	}
	buffer := make([]uint16, needed)
	written, err := windows.MultiByteToWideChar(cpOEM, 0, &data[0], int32(len(data)), &buffer[0], needed)
	if err != nil || written <= 0 {
		return string(data)
	}
	return windows.UTF16ToString(buffer[:written])
}

// CreateArgs 构造 schtasks /Create 的参数，独立成函数便于测试。
func CreateArgs(taskName, exePath string) []string {
	tr := fmt.Sprintf("\"%s\" check --notify", exePath)
	return []string{
		"/Create",
		"/TN", taskName,
		"/SC", "HOURLY",
		"/MO", "1",
		"/TR", tr,
		"/F",
	}
}

// Install 注册每小时执行一次 check --notify 的计划任务。
func Install(taskName, exePath string) error {
	if taskName == "" {
		taskName = DefaultTaskName
	}
	output, err := runCommand("schtasks.exe", CreateArgs(taskName, exePath)...)
	if err != nil {
		return fmt.Errorf("schtasks /Create 失败: %w: %s", err, strings.TrimSpace(output))
	}
	return nil
}

// IsUnstablePath 判断 exe 是否来自 go run 的临时构建目录。
func IsUnstablePath(exePath string) bool {
	lower := strings.ToLower(exePath)
	return strings.Contains(lower, `\temp\go-build`) || strings.Contains(lower, `\go-build`)
}

// Uninstall 删除计划任务；任务不存在时返回 removed=false 且不报错。
func Uninstall(taskName string) (bool, error) {
	if taskName == "" {
		taskName = DefaultTaskName
	}
	installed, _, err := Query(taskName)
	if err != nil {
		return false, err
	}
	if !installed {
		return false, nil
	}
	output, err := runCommand("schtasks.exe", "/Delete", "/TN", taskName, "/F")
	if err != nil {
		return false, fmt.Errorf("schtasks /Delete 失败: %w: %s", err, strings.TrimSpace(output))
	}
	return true, nil
}

// Query 查询计划任务是否存在，并返回可读的详细输出。
func Query(taskName string) (bool, string, error) {
	if taskName == "" {
		taskName = DefaultTaskName
	}
	output, err := runCommand("schtasks.exe", "/Query", "/TN", taskName, "/FO", "LIST", "/V")
	if err != nil {
		// schtasks 对「任务不存在」返回非零退出码；此时按未安装处理。
		return false, strings.TrimSpace(output), nil
	}
	return true, strings.TrimSpace(output), nil
}

// NextRun 从 schtasks 详细输出中尽力提取下次运行时间；提取不到返回空串。
func NextRun(detail string) string {
	for _, line := range strings.Split(detail, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "Next Run Time" || key == "下次运行时间" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
