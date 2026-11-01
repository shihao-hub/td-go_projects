//go:build !windows

package procguard

// Guard 在非 Windows 平台为空实现；POSIX 下的子进程回收由进程组与会话负责。
type Guard struct{}

// Attach 空实现。
func Attach(pid int) (*Guard, error) { return &Guard{}, nil }

// Close 空实现。
func (g *Guard) Close() error { return nil }
