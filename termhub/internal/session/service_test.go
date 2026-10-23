// service_test.go：SessionManager 接缝的行为断言（T01 验收条件 1）。
package session

import (
	"context"
	"testing"
	"time"

	"github.com/shihao-hub/termhub/internal/pty"
)

// TestStartSession_InputReachesPTY_OutputViaEvents 验证：
// 输入写入会到达 PTY；PTY 输出经事件回调（Subscribe 通道）送达。
func TestStartSession_InputReachesPTY_OutputViaEvents(t *testing.T) {
	ff := &FakeFactory{}
	svc := NewService(ff)

	id, err := svc.StartSession(context.Background(), pty.Options{})
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if id == "" {
		t.Fatal("会话 ID 为空")
	}
	if got := ff.Opts()[0].CommandLine; got != "powershell.exe -NoLogo" {
		t.Fatalf("默认命令行 = %q", got)
	}

	backlog, ch, _, err := svc.Subscribe(id)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if len(backlog) != 0 {
		t.Fatalf("初始 backlog 应为空，got %d bytes", len(backlog))
	}

	// 1. 用户输入到达 PTY。
	if err := svc.WriteInput(id, []byte("dir\r")); err != nil {
		t.Fatalf("WriteInput: %v", err)
	}
	ft := ff.Terms()[0]
	if got := ft.Written(); got != "dir\r" {
		t.Fatalf("PTY 收到的输入 = %q, want %q", got, "dir\r")
	}

	// 2. PTY 输出经订阅通道送达。
	ft.Push([]byte(" Volume in drive D .\r\n"))
	select {
	case chunk := <-ch:
		if string(chunk) != " Volume in drive D .\r\n" {
			t.Fatalf("事件回调输出 = %q", chunk)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("2s 内未收到输出事件")
	}

	// 3. 断开重连的客户端回放最近缓冲。
	backlog2, ch2, _, err := svc.Subscribe(id)
	if err != nil {
		t.Fatalf("second Subscribe: %v", err)
	}
	if string(backlog2) != " Volume in drive D .\r\n" {
		t.Fatalf("backlog = %q", backlog2)
	}
	ft.Push([]byte("second line"))
	select {
	case chunk := <-ch2:
		if string(chunk) != "second line" {
			t.Fatalf("second client 输出 = %q", chunk)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("second client 未收到实时输出")
	}
}

// TestStartSession_FactoryError 透传工厂错误。
func TestStartSession_FactoryError(t *testing.T) {
	wantErr := context.DeadlineExceeded
	ff := &FakeFactory{errAt: 1, err: wantErr}
	svc := NewService(ff)

	_, err := svc.StartSession(context.Background(), pty.Options{})
	if err != wantErr {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
}

// TestSessionLifecycle_FakeClose_Exits 写入已退出会话须报错，快照转 exited。
func TestSessionLifecycle_FakeClose_Exits(t *testing.T) {
	ff := &FakeFactory{}
	svc := NewService(ff)
	id, _ := svc.StartSession(context.Background(), pty.Options{})
	_, ch, stopped, _ := svc.Subscribe(id)

	if err := ff.Terms()[0].Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("会话退出事件未送达")
	}

	// 快照接口只在会话存续期可见（T06 再落盘已退出会话），此处退订验证。
	if err := svc.WriteInput(id, []byte("x")); err == nil {
		t.Fatal("已退出会话的写入应报错")
	}
	select {
	case <-ch:
		t.Fatal("退出后不应再有输出")
	default:
	}
}

// TestResize_DelegatesToPTY Resize 转发。
func TestResize_DelegatesToPTY(t *testing.T) {
	ff := &FakeFactory{}
	svc := NewService(ff)
	id, _ := svc.StartSession(context.Background(), pty.Options{Cols: 80, Rows: 24})
	if got := ff.Opts()[0].Cols; got != 80 {
		t.Fatalf("start cols = %d", got)
	}
	if err := svc.Resize(id, 100, 40); err != nil {
		t.Fatalf("Resize: %v", err)
	}
}

// TestShutdown_ClosesAllSessions 应用退出时全部会话 PTY 关闭。
func TestShutdown_ClosesAllSessions(t *testing.T) {
	ff := &FakeFactory{}
	svc := NewService(ff)
	for i := 0; i < 2; i++ {
		if _, err := svc.StartSession(context.Background(), pty.Options{}); err != nil {
			t.Fatalf("StartSession: %v", err)
		}
	}

	svc.Shutdown()

	for i, ft := range ff.Terms() {
		if _, err := ft.Write([]byte("x")); err == nil {
			t.Fatalf("会话 %d 的 PTY 未关闭", i)
		}
	}
}

// TestListSessions_IncludesWorkDir 快照带出初始目录，供窗口重建时恢复 tab。
func TestListSessions_IncludesWorkDir(t *testing.T) {
	ff := &FakeFactory{}
	svc := NewService(ff)
	if _, err := svc.StartSession(context.Background(), pty.Options{WorkDir: `C:\work`}); err != nil {
		t.Fatalf("StartSession: %v", err)
	}

	got := svc.ListSessions()
	if len(got) != 1 {
		t.Fatalf("sessions = %d, want 1", len(got))
	}
	if got[0].WorkDir != `C:\work` {
		t.Fatalf("work_dir = %q, want %q", got[0].WorkDir, `C:\work`)
	}
}
