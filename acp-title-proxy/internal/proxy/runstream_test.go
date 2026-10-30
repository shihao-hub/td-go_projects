package proxy

import (
	"io"
	"testing"
	"time"
)

// closedStream 模拟一个立即 EOF 的双工流（会话断开时的典型状态）。
type closedStream struct{}

func (closedStream) Read(p []byte) (int, error)  { return 0, io.EOF }
func (closedStream) Write(p []byte) (int, error) { return len(p), nil }
func (closedStream) Close() error                { return nil }

// TestRunStreamReturnsWhenStreamClosed 验证客户端 stdin 始终不关闭时，
// RunStream 仍能在流结束后及时返回。
// 回归背景：入向 goroutine 曾纳入 WaitGroup，会话重建后进程会永久阻塞在 stdin 读取上，
// 每次重建都残留一个僵尸代理进程。
func TestRunStreamReturnsWhenStreamClosed(t *testing.T) {
	stdinReader, stdinWriter := io.Pipe()
	defer stdinWriter.Close() // 写端保持打开，模拟 Zed 重建会话时未关闭旧管道

	runner := &Runner{Stdin: stdinReader}
	inj := NewInjector(io.Discard)

	done := make(chan error, 1)
	go func() {
		done <- runner.RunStream(closedStream{}, inj)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RunStream 返回错误: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RunStream 在流关闭后仍未返回（退出路径被阻塞的 stdin goroutine 拖住）")
	}
}
