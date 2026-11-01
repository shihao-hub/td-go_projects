//go:build !windows

package governor

import (
	"os"
	"sync"
	"syscall"
)

type otherProcessLock struct {
	mu sync.Mutex
}

func defaultProcessLock() crossProcessLock {
	return &otherProcessLock{}
}

func (l *otherProcessLock) Lock() error {
	l.mu.Lock()
	return nil
}

func (l *otherProcessLock) Unlock() {
	l.mu.Unlock()
}

func defaultIsProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}
