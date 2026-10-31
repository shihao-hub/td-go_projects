package mux

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"language_projects/go_projects/acp-title-proxy/internal/config"
	"language_projects/go_projects/acp-title-proxy/internal/protocol"
)

// ---------- fake agent ----------

type fakeAgent struct {
	h Handler

	inR  *io.PipeReader
	inW  *io.PipeWriter
	outR *io.PipeReader
	outW *io.PipeWriter

	mu          sync.Mutex
	nextSession int
	initCount   int
	waiters     map[string]chan protocol.Message
	permissions map[string]string
	received    []protocol.Message

	closeOnce sync.Once
}

func newFakeAgent(h Handler) *fakeAgent {
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	f := &fakeAgent{
		h:           h,
		inR:         inR,
		inW:         inW,
		outR:        outR,
		outW:        outW,
		waiters:     make(map[string]chan protocol.Message),
		permissions: make(map[string]string),
	}
	go f.pumpOut()
	go f.readLoop()
	return f
}

func (f *fakeAgent) pumpOut() {
	r := bufio.NewReader(f.outR)
	for {
		line, err := r.ReadBytes('\n')
		line = bytes.TrimRight(line, "\r\n")
		if len(line) > 0 {
			f.h.OnLine(line)
		}
		if err != nil {
			return
		}
	}
}

func (f *fakeAgent) readLoop() {
	r := bufio.NewReader(f.inR)
	for {
		line, err := r.ReadBytes('\n')
		line = bytes.TrimRight(line, "\r\n")
		if len(line) > 0 {
			f.handle(line)
		}
		if err != nil {
			return
		}
	}
}

func (f *fakeAgent) handle(line []byte) {
	m, err := protocol.ParseMessage(line)
	if err != nil {
		return
	}

	f.mu.Lock()
	f.received = append(f.received, m)
	f.mu.Unlock()

	switch {
	case m.IsResponse():
		f.mu.Lock()
		ch := f.waiters[protocol.IDString(m.ID)]
		f.mu.Unlock()
		if ch != nil {
			select {
			case ch <- m:
			default:
			}
		}

	case m.IsRequest():
		switch m.Method {
		case "initialize":
			f.mu.Lock()
			f.initCount++
			f.mu.Unlock()
			f.respond(m.ID, map[string]any{
				"protocolVersion": 1,
				"agentInfo":       map[string]any{"name": "fake-agent", "version": "0.0.1"},
			})
		case "session/new":
			f.mu.Lock()
			f.nextSession++
			sid := fmt.Sprintf("sess-%d", f.nextSession)
			f.mu.Unlock()
			f.respond(m.ID, map[string]any{"sessionId": sid})
		case "session/prompt":
			go f.runPrompt(m.ID, m.SessionID())
		default:
			f.respond(m.ID, map[string]any{})
		}
	}
}

func (f *fakeAgent) runPrompt(promptID json.RawMessage, sid string) {
	f.emit(protocol.Notification("session/update", map[string]any{
		"sessionId": sid,
		"update": map[string]any{
			"sessionUpdate": "agent_message_chunk",
			"content":       map[string]any{"type": "text", "text": "hi"},
		},
	}))

	revID := "rev-" + sid + "-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	ch := make(chan protocol.Message, 1)
	f.mu.Lock()
	f.waiters[revID] = ch
	f.mu.Unlock()

	f.emit(map[string]any{
		"jsonrpc": "2.0",
		"id":      revID,
		"method":  "session/request_permission",
		"params": map[string]any{
			"sessionId": sid,
			"toolCall":  map[string]any{"toolCallId": "t-" + sid, "title": "fake tool"},
		},
	})

	select {
	case resp := <-ch:
		var r struct {
			OptionID string `json:"optionId"`
		}
		_ = json.Unmarshal(resp.Result, &r)
		f.mu.Lock()
		f.permissions[sid] = r.OptionID
		f.mu.Unlock()
		f.respond(promptID, map[string]any{"ok": true})
	case <-time.After(3 * time.Second):
		f.respondError(promptID, -32000, "permission timeout")
	}
}

func (f *fakeAgent) emit(v any) {
	b, _ := json.Marshal(v)
	_, _ = f.outW.Write(append(b, '\n'))
}

func (f *fakeAgent) respond(id json.RawMessage, result any) {
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	_, _ = f.outW.Write(append(b, '\n'))
}

func (f *fakeAgent) respondError(id json.RawMessage, code int, msg string) {
	b, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"error":   map[string]any{"code": code, "message": msg},
	})
	_, _ = f.outW.Write(append(b, '\n'))
}

func (f *fakeAgent) WriteLine(line []byte) error {
	_, err := f.inW.Write(append(line, '\n'))
	return err
}

func (f *fakeAgent) Close() error {
	f.closeOnce.Do(func() {
		_ = f.inW.Close()
		_ = f.outW.Close()
	})
	return nil
}

func (f *fakeAgent) PID() int { return 4242 }

// ---------- test client ----------

type testClient struct {
	t    *testing.T
	conn net.Conn
	r    *bufio.Reader
	next int
}

func newTestClient(t *testing.T, port int) *testClient {
	t.Helper()
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial %s: %v", addr, err)
	}
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := conn.Write([]byte(`{"cmd":"attach","proto":1}` + "\n")); err != nil {
		t.Fatalf("attach write: %v", err)
	}
	r := bufio.NewReaderSize(conn, 1<<20)
	line, err := r.ReadBytes('\n')
	if err != nil {
		t.Fatalf("attach read: %v", err)
	}
	if !strings.Contains(string(line), `"ok"`) {
		t.Fatalf("attach rejected: %s", line)
	}
	_ = conn.SetDeadline(time.Time{})
	return &testClient{t: t, conn: conn, r: r}
}

func (c *testClient) write(v any) {
	b, _ := json.Marshal(v)
	_, _ = c.conn.Write(append(b, '\n'))
}

func (c *testClient) writeRaw(b []byte) {
	_, _ = c.conn.Write(append(b, '\n'))
}

func (c *testClient) read(timeout time.Duration) (protocol.Message, bool) {
	_ = c.conn.SetReadDeadline(time.Now().Add(timeout))
	line, err := c.r.ReadBytes('\n')
	if err != nil {
		return protocol.Message{}, false
	}
	m, err := protocol.ParseMessage(bytes.TrimRight(line, "\r\n"))
	if err != nil {
		return protocol.Message{}, false
	}
	return m, true
}

func (c *testClient) request(method string, params any) protocol.Message {
	c.next++
	id := c.next
	c.write(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	return c.awaitResponse(id, 5*time.Second)
}

func (c *testClient) awaitResponse(id int, timeout time.Duration) protocol.Message {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		m, ok := c.read(time.Until(deadline))
		if !ok {
			return protocol.Message{}
		}
		switch {
		case m.IsRequest():
			c.writeRaw(protocol.ErrorResponse(m.ID, -32601, "test client does not implement"))
		case m.IsNotification():
			continue
		case m.IsResponse():
			if protocol.IDString(m.ID) == strconv.Itoa(id) {
				return m
			}
		}
	}
	return protocol.Message{}
}

// prompt 发起 session/prompt，并自动响应途中收到的权限请求。
func (c *testClient) prompt(sid, optionID string) (protocol.Message, []protocol.Message) {
	c.next++
	id := c.next
	c.write(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "session/prompt",
		"params": map[string]any{
			"sessionId": sid,
			"prompt":    []any{map[string]any{"type": "text", "text": "hi"}},
		},
	})

	var notes []protocol.Message
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		m, ok := c.read(time.Until(deadline))
		if !ok {
			return protocol.Message{}, notes
		}
		switch {
		case m.IsRequest() && m.Method == "session/request_permission":
			c.writeRaw(protocol.ResultResponse(m.ID, json.RawMessage(fmt.Sprintf(`{"optionId":%q}`, optionID))))
		case m.IsNotification():
			notes = append(notes, m)
		case m.IsResponse() && protocol.IDString(m.ID) == strconv.Itoa(id):
			return m, notes
		}
	}
	return protocol.Message{}, notes
}

// ---------- harness ----------

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("free port: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func waitPort(t *testing.T, port int) {
	t.Helper()
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	for i := 0; i < 100; i++ {
		c, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			_ = c.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("daemon did not listen on %s", addr)
}

func startTestDaemon(t *testing.T, maxSessions int) (*Daemon, *fakeAgent, int) {
	t.Helper()
	port := freePort(t)
	cfg := config.DefaultConfig()
	cfg.Share.Port = port
	cfg.Share.MaxSessions = maxSessions

	var fake *fakeAgent
	d := NewDaemon(cfg, log.New(io.Discard, "", 0), func(h Handler) (Conn, error) {
		fake = newFakeAgent(h)
		return fake, nil
	})
	go func() { _ = d.Run() }()
	waitPort(t, port)
	if fake == nil || fake.h == nil {
		t.Fatal("fake backend not created")
	}
	t.Cleanup(d.Shutdown)
	return d, fake, port
}

func initParams() map[string]any {
	return map[string]any{
		"protocolVersion": 1,
		"clientInfo":      map[string]any{"name": "test-client", "version": "0"},
		"clientCapabilities": map[string]any{
			"fs":       map[string]any{"readTextFile": true, "writeTextFile": true},
			"terminal": true,
		},
	}
}

// ---------- tests ----------

func TestMultiSessionRouting(t *testing.T) {
	_, fake, port := startTestDaemon(t, 4)

	a := newTestClient(t, port)
	defer a.conn.Close()
	b := newTestClient(t, port)
	defer b.conn.Close()

	if resp := a.request("initialize", initParams()); len(resp.Error) > 0 {
		t.Fatalf("A initialize failed: %s", resp.Error)
	}
	if resp := b.request("initialize", initParams()); len(resp.Error) > 0 {
		t.Fatalf("B initialize failed: %s", resp.Error)
	}

	respA := a.request("session/new", map[string]any{"cwd": `C:\a`, "mcpServers": []any{}})
	if len(respA.Error) > 0 {
		t.Fatalf("A session/new failed: %s", respA.Error)
	}
	respB := b.request("session/new", map[string]any{"cwd": `C:\b`, "mcpServers": []any{}})
	if len(respB.Error) > 0 {
		t.Fatalf("B session/new failed: %s", respB.Error)
	}

	sidA := protocol.ExtractSessionID(respA.Result)
	sidB := protocol.ExtractSessionID(respB.Result)
	if sidA == "" || sidB == "" || sidA == sidB {
		t.Fatalf("unexpected session ids: A=%q B=%q", sidA, sidB)
	}

	// 两个 client 都使用 id=2 发 session/new，验证 daemon 的 id 翻译不冲突。
	if protocol.IDString(respA.ID) != "2" || protocol.IDString(respB.ID) != "2" {
		t.Fatalf("response ids not restored: A=%s B=%s", respA.ID, respB.ID)
	}

	promptA, notesA := a.prompt(sidA, "allow-a")
	if len(promptA.Error) > 0 {
		t.Fatalf("A prompt failed: %s", promptA.Error)
	}
	promptB, notesB := b.prompt(sidB, "allow-b")
	if len(promptB.Error) > 0 {
		t.Fatalf("B prompt failed: %s", promptB.Error)
	}

	for _, n := range notesA {
		if n.SessionID() != sidA {
			t.Errorf("A received notification for %q", n.SessionID())
		}
	}
	for _, n := range notesB {
		if n.SessionID() != sidB {
			t.Errorf("B received notification for %q", n.SessionID())
		}
	}

	fake.mu.Lock()
	gotA := fake.permissions[sidA]
	gotB := fake.permissions[sidB]
	initCount := fake.initCount
	fake.mu.Unlock()

	if gotA != "allow-a" || gotB != "allow-b" {
		t.Fatalf("permission routed wrong: A=%q B=%q", gotA, gotB)
	}
	if initCount != 1 {
		t.Fatalf("initialize should be cached, backend saw %d calls", initCount)
	}
}

func TestCapabilitiesMismatchRejected(t *testing.T) {
	_, _, port := startTestDaemon(t, 4)

	a := newTestClient(t, port)
	defer a.conn.Close()
	if resp := a.request("initialize", initParams()); len(resp.Error) > 0 {
		t.Fatalf("A initialize failed: %s", resp.Error)
	}

	b := newTestClient(t, port)
	defer b.conn.Close()
	other := map[string]any{
		"protocolVersion":    1,
		"clientInfo":         map[string]any{"name": "other", "version": "0"},
		"clientCapabilities": map[string]any{"terminal": false},
	}
	resp := b.request("initialize", other)
	if len(resp.Error) == 0 {
		t.Fatalf("expected capabilities mismatch error, got: %s", resp.Result)
	}
	if !bytes.Contains(resp.Error, []byte("-32001")) {
		t.Fatalf("unexpected error: %s", resp.Error)
	}
}

func TestMaxSessionsRejected(t *testing.T) {
	_, _, port := startTestDaemon(t, 1)

	a := newTestClient(t, port)
	defer a.conn.Close()
	if resp := a.request("initialize", initParams()); len(resp.Error) > 0 {
		t.Fatalf("A initialize failed: %s", resp.Error)
	}
	if resp := a.request("session/new", map[string]any{"cwd": `C:\a`, "mcpServers": []any{}}); len(resp.Error) > 0 {
		t.Fatalf("A session/new failed: %s", resp.Error)
	}

	b := newTestClient(t, port)
	defer b.conn.Close()
	if resp := b.request("initialize", initParams()); len(resp.Error) > 0 {
		t.Fatalf("B initialize failed: %s", resp.Error)
	}
	resp := b.request("session/new", map[string]any{"cwd": `C:\b`, "mcpServers": []any{}})
	if len(resp.Error) == 0 {
		t.Fatalf("expected max_sessions error")
	}
	if !bytes.Contains(resp.Error, []byte("-32002")) {
		t.Fatalf("unexpected error: %s", resp.Error)
	}
}
