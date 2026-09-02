//go:build !windows

package app

func AcquireSingleInstance() bool { return true }
