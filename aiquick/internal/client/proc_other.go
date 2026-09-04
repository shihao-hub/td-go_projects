//go:build !windows

package client

import "syscall"

func procAttr() *syscall.SysProcAttr { return nil }
