//go:build !windows

package main

// attachParentConsole 在非 Windows 平台不存在控制台窗口问题，保持空实现。
func attachParentConsole() {}
