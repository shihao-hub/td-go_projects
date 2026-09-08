//go:build windows

package lock

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

const lockOffset = int64(1 << 30)

const (
	lockfileFailImmediately = 0x00000001
	lockfileExclusiveLock   = 0x00000002
	errnoLockViolation      = 33
)

var (
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	procLockFileEx   = kernel32.NewProc("LockFileEx")
	procUnlockFileEx = kernel32.NewProc("UnlockFileEx")
)

func lockOverlapped() *syscall.Overlapped {
	return &syscall.Overlapped{Offset: uint32(lockOffset), OffsetHigh: uint32(lockOffset >> 32)}
}

func lockHandle(f *os.File) error {
	r1, _, err := procLockFileEx.Call(f.Fd(),
		lockfileExclusiveLock|lockfileFailImmediately, 0, 1, 0,
		uintptr(unsafe.Pointer(lockOverlapped())))
	if r1 == 0 {
		if errno, ok := err.(syscall.Errno); ok && errno == errnoLockViolation {
			return fmt.Errorf("%s: %w", f.Name(), ErrHeld)
		}
		return fmt.Errorf("LockFileEx %s: %w", f.Name(), err)
	}
	return nil
}

func unlockHandle(f *os.File) error {
	r1, _, err := procUnlockFileEx.Call(f.Fd(), 0, 1, 0,
		uintptr(unsafe.Pointer(lockOverlapped())))
	if r1 == 0 {
		return fmt.Errorf("UnlockFileEx %s: %w", f.Name(), err)
	}
	return nil
}
