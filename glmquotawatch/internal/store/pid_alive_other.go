//go:build !windows

package store

// pidAlive 非 Windows 平台不探测（本项目面向 Windows，此实现仅为交叉编译兜底）。
func pidAlive(pid int) bool { return true }
