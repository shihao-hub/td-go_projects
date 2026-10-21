//go:build !windows

package appenv

import "syscall"

// processAlive 判断 PID 对应的进程是否仍在运行（信号 0 探测）。
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return syscall.Kill(pid, 0) == nil
}
