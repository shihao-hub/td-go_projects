//go:build !windows

package scheduler

import "errors"

// DefaultTaskName 是计划任务的默认名称。
const DefaultTaskName = "rebootremind_check"

var errUnsupported = errors.New("rebootremind 仅支持 Windows")

// ErrUnstableExecutable 在非 Windows 平台保留同名错误。
var ErrUnstableExecutable = errUnsupported

// Install 在非 Windows 平台不可用。
func Install(taskName, exePath string) error { return errUnsupported }

// IsUnstablePath 在非 Windows 平台返回 false。
func IsUnstablePath(exePath string) bool { return false }

// Uninstall 在非 Windows 平台不可用。
func Uninstall(taskName string) (bool, error) { return false, errUnsupported }

// Query 在非 Windows 平台不可用。
func Query(taskName string) (bool, string, error) { return false, "", errUnsupported }

// NextRun 在非 Windows 平台返回空串。
func NextRun(detail string) string { return "" }
