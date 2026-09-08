//go:build !windows

package lock

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

func lockHandle(f *os.File) error {
	err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return fmt.Errorf("%s: %w", f.Name(), ErrHeld)
		}
		return fmt.Errorf("flock %s: %w", f.Name(), err)
	}
	return nil
}

func unlockHandle(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
