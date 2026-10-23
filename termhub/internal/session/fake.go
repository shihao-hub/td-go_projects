// fake.go：单测用 fake PTY 工厂与 Term 实现，替代真 ConPTY。
package session

import (
	"bytes"
	"fmt"
	"sync"

	"github.com/shihao-hub/termhub/internal/pty"
)

// FakeTerm 记录写输入的内存 PTY；测试可通过 Push 注入输出。
type FakeTerm struct {
	mu     sync.Mutex
	writes bytes.Buffer
	closed bool

	// readC 收到 Push 的数据后交给 Read 消费。
	readC chan []byte
	// closeErr Close 时返回的错误（模拟进程终止语义）。
	closeErr error
}

// NewFakeTerm 空 fake。
func NewFakeTerm() *FakeTerm {
	return &FakeTerm{readC: make(chan []byte, 16)}
}

// Push 注入一段终端输出。
func (f *FakeTerm) Push(b []byte) { f.readC <- append([]byte(nil), b...) }

// Read 消费 Push 的数据；通道关闭时尚未取走的数据先交付、随后返回 EOF。
func (f *FakeTerm) Read(p []byte) (int, error) {
	// 惰性关闭：读到关闭信号后将其余数据交完再 EOF，由工厂统一管理关闭。
	for {
		b, ok := <-f.readC
		if !ok {
			return 0, fmt.Errorf("pty eof")
		}
		n := copy(p, b)
		if n < len(b) {
			// 一次装不下，剩余塞回去（测试注入量小，通常不发生）
			go func(rest []byte) { f.readC <- rest }(b[n:])
		}
		return n, nil
	}
}

// Write 记录写入的输入。
func (f *FakeTerm) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return 0, fmt.Errorf("pty closed")
	}
	return f.writes.Write(p)
}

// Written 快照断言用。
func (f *FakeTerm) Written() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.writes.String()
}

// Resize no-op。
func (f *FakeTerm) Resize(cols, rows int) error { return nil }

// Close 关闭读通道模拟进程终止。
func (f *FakeTerm) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return f.closeErr
	}
	f.closed = true
	close(f.readC)
	return f.closeErr
}

// Pid fake 恒 0。
func (f *FakeTerm) Pid() int { return 0 }

// FakeFactory 每次 Start 记录参数并按需返回新 fake 或 Start 错误。
type FakeFactory struct {
	mu    sync.Mutex
	made  []*FakeTerm
	opts  []pty.Options
	errAt int // 第几次 Start（1 起）返回错误；0 表示永不
	err   error
}

// Start 实现 pty.Factory。
func (ff *FakeFactory) Start(opts pty.Options) (pty.Term, error) {
	ff.mu.Lock()
	defer ff.mu.Unlock()
	if ff.errAt > 0 && len(ff.made)+1 == ff.errAt {
		return nil, ff.err
	}
	ft := NewFakeTerm()
	ff.made = append(ff.made, ft)
	ff.opts = append(ff.opts, opts)
	return ft, nil
}

// Terms 已创建的 fake 列表。
func (ff *FakeFactory) Terms() []*FakeTerm {
	ff.mu.Lock()
	defer ff.mu.Unlock()
	return append([]*FakeTerm(nil), ff.made...)
}

// Opts 已收到的启动参数列表。
func (ff *FakeFactory) Opts() []pty.Options {
	ff.mu.Lock()
	defer ff.mu.Unlock()
	return append([]pty.Options(nil), ff.opts...)
}
