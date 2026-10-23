// service_test.go：SessionManager 接缝的行为断言（T01 验收条件 1）。
package session

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/shihao-hub/termhub/internal/pty"
	"github.com/shihao-hub/termhub/internal/store"
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

// TestStopSession_ClosesPTY 关闭 tab 会终止 PTY 并广播退出。
func TestStopSession_ClosesPTY(t *testing.T) {
	ff := &FakeFactory{}
	svc := NewService(ff)
	id, err := svc.StartSession(context.Background(), pty.Options{})
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	_, _, stopped, err := svc.Subscribe(id)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if err := svc.StopSession(id); err != nil {
		t.Fatalf("StopSession: %v", err)
	}
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("StopSession 后未收到退出事件")
	}
	if _, err := ff.Terms()[0].Write([]byte("x")); err == nil {
		t.Fatal("PTY 应已关闭")
	}
}

// TestSessionHistoryPersisted 会话命令写入历史并落盘为已退出记录。
func TestSessionHistoryPersisted(t *testing.T) {
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	ff := &FakeFactory{}
	svc := NewServiceWithStore(ff, st)
	id, err := svc.StartSession(context.Background(), pty.Options{WorkDir: `C:\work`, InitialCommand: "dir"})
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if got := ff.Opts()[0].CommandLine; !strings.Contains(got, "-NoExit") || !strings.Contains(got, "-File") {
		t.Fatalf("实盘命令行应带历史钩子: %q", got)
	}
	if got := ff.Opts()[0].InitialCommand; got != "dir" {
		t.Fatalf("InitialCommand = %q", got)
	}

	path := st.History().SessionPath(id)
	if err := os.WriteFile(path, []byte("dir\ngit status\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, stopped, _ := svc.Subscribe(id)
	if err := svc.StopSession(id); err != nil {
		t.Fatalf("StopSession: %v", err)
	}
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("等待会话退出超时")
	}

	records := st.Sessions()
	if len(records) != 1 {
		t.Fatalf("sessions = %+v", records)
	}
	if got := records[0].Commands; len(got) != 2 || got[1] != "git status" {
		t.Fatalf("commands = %v", got)
	}
	if got := st.History().Global(); len(got) < 2 || got[len(got)-1] != "git status" {
		t.Fatalf("global history = %v", got)
	}
}

// TestBuildBootstrap 启动脚本包含目录、历史钩子、全局历史与初始命令。
func TestBuildBootstrap(t *testing.T) {
	script := buildBootstrap(`C:\work`, `C:\h.txt`, `C:\cwd.txt`, `C:\global.txt`, "git status")
	for _, want := range []string{"Set-Location", "__termhub_history", "PSConsoleHostReadLine", "HistorySavePath", "git status"} {
		if !strings.Contains(script, want) {
			t.Fatalf("bootstrap 缺少 %q:\n%s", want, script)
		}
	}
}

func TestStripANSI(t *testing.T) {
	got := stripANSI("\x1b[31merror\x1b[0m\r\n")
	if got != "error\r\n" {
		t.Fatalf("stripANSI = %q", got)
	}
}

// TestShutdown_PersistsSessions 应用整体退出前也把运行中会话落盘为已退出记录。
func TestShutdown_PersistsSessions(t *testing.T) {
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	ff := &FakeFactory{}
	svc := NewServiceWithStore(ff, st)
	id, err := svc.StartSession(context.Background(), pty.Options{WorkDir: `C:\work`})
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if err := os.WriteFile(st.History().SessionPath(id), []byte("dir\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	svc.Shutdown()
	records := st.Sessions()
	if len(records) != 1 {
		t.Fatalf("sessions = %+v", records)
	}
	if got := records[0].Commands; len(got) != 1 || got[0] != "dir" {
		t.Fatalf("commands = %v", got)
	}
}

// TestSnapshot_IncludesCwd 快照读取 prompt 钩子落盘的当前目录。
func TestSnapshot_IncludesCwd(t *testing.T) {
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ff := &FakeFactory{}
	svc := NewServiceWithStore(ff, st)
	id, err := svc.StartSession(context.Background(), pty.Options{WorkDir: `C:\work`})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(st.History().SessionCwdPath(id), []byte(`C:\work\sub`), 0o644); err != nil {
		t.Fatal(err)
	}
	snaps := svc.ListSessions()
	if len(snaps) != 1 || snaps[0].Cwd != `C:\work\sub` {
		t.Fatalf("snapshots = %+v", snaps)
	}
	svc.Shutdown()
}

// TestAskAgent_PromptAndConversation 诊断 prompt 含上下文，追问复用 conversation id。
func TestAskAgent_PromptAndConversation(t *testing.T) {
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AppendSession(store.SessionRecord{
		ID: "s-exit", WorkDir: `C:\work`, Status: "exited",
		Commands: []string{"dir", "git status"}, LastOutput: "error: not found",
	}); err != nil {
		t.Fatal(err)
	}
	svc := NewServiceWithStore(&FakeFactory{}, st)
	var calls [][]string
	svc.AgentRunner = func(_ context.Context, _ string, args []string) ([]byte, error) {
		calls = append(calls, append([]string(nil), args...))
		return []byte(`{"conversation_id":"c-1","response":"建议执行 dir","status":"SUCCESS"}`), nil
	}

	answer, convo, err := svc.AskAgent(context.Background(), "s-exit", "为什么失败？", "")
	if err != nil {
		t.Fatalf("AskAgent: %v", err)
	}
	if answer != "建议执行 dir" || convo != "c-1" {
		t.Fatalf("answer=%q convo=%q", answer, convo)
	}
	prompt := calls[0][1]
	for _, want := range []string{"git status", "error: not found", "为什么失败？", `C:\work`} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt 缺少 %q:\n%s", want, prompt)
		}
	}
	if _, convo, err := svc.AskAgent(context.Background(), "s-exit", "继续", ""); err != nil || convo != "c-1" {
		t.Fatalf("second AskAgent err=%v convo=%q", err, convo)
	}
	second := calls[1]
	found := false
	for i := 0; i < len(second)-1; i++ {
		if second[i] == "--conversation" && second[i+1] == "c-1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("追问未复用 conversation id: %v", second)
	}
}

// TestAskAgent_Error agent 调用失败时返回明确错误。
func TestAskAgent_Error(t *testing.T) {
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AppendSession(store.SessionRecord{ID: "s1", WorkDir: `C:\work`}); err != nil {
		t.Fatal(err)
	}
	svc := NewServiceWithStore(&FakeFactory{}, st)
	svc.AgentRunner = func(context.Context, string, []string) ([]byte, error) {
		return nil, errors.New("agy not found")
	}
	if _, _, err := svc.AskAgent(context.Background(), "s1", "", ""); err == nil {
		t.Fatal("应返回 agent 调用失败")
	}
}
