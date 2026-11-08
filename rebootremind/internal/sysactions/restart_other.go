//go:build !windows

package sysactions

import "errors"

// Restart 在非 Windows 平台不可用。
func Restart() error { return errors.New("rebootremind 仅支持 Windows") }
