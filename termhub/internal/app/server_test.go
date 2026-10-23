// server_test.go：终端记忆编排层的行为断言（fake PTY 驱动）。
package app

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shihao-hub/termhub/internal/session"
	"github.com/shihao-hub/termhub/internal/store"
)

// postJSON 通过 server 路由发起 JSON 请求并解析统一响应。
func postJSON(t *testing.T, srv *Server, method, path, body string) (*httptest.ResponseRecorder, APIResponse) {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	var resp APIResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	return rec, resp
}

func terminals(t *testing.T, srv *Server) []terminalView {
	t.Helper()
	rec, resp := postJSON(t, srv, http.MethodGet, "/api/v1/terminals", "")
	if rec.Code != http.StatusOK || !resp.OK {
		t.Fatalf("GET terminals: status=%d resp=%+v", rec.Code, resp)
	}
	raw, err := json.Marshal(resp.Data)
	if err != nil {
		t.Fatal(err)
	}
	var views []terminalView
	if err := json.Unmarshal(raw, &views); err != nil {
		t.Fatal(err)
	}
	return views
}

// newTestServer 装配 fake PTY 的编排实例与存储。
func newTestServer(t *testing.T) (*Server, *session.FakeFactory, *store.Store) {
	t.Helper()
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	ff := &session.FakeFactory{}
	srv := newServerWithStore(st, ff, nil)
	return srv, ff, st
}

// dirBody 生成指向真实临时目录的创建请求体。
func dirBody(t *testing.T, dir string) string {
	t.Helper()
	raw, _ := json.Marshal(map[string]string{"work_dir": dir})
	return string(raw)
}

func TestTerminalLifecycle_CreateRunDelete(t *testing.T) {
	srv, ff, st := newTestServer(t)
	workDir := t.TempDir()

	// 新建终端 → 一条记忆 + 一个运行中 shell（不带初始命令）。
	rec, resp := postJSON(t, srv, http.MethodPost, "/api/v1/terminals", dirBody(t, workDir))
	if rec.Code != http.StatusOK || !resp.OK {
		t.Fatalf("create: status=%d resp=%+v", rec.Code, resp)
	}
	var created struct {
		SlotID    string `json:"slot_id"`
		SessionID string `json:"session_id"`
	}
	if raw, err := json.Marshal(resp.Data); err != nil {
		t.Fatal(err)
	} else if err := json.Unmarshal(raw, &created); err != nil {
		t.Fatal(err)
	}
	if created.SlotID == "" || created.SessionID == "" {
		t.Fatalf("created = %+v", created)
	}

	views := terminals(t, srv)
	if len(views) != 1 {
		t.Fatalf("terminals = %+v", views)
	}
	if views[0].Session == nil || views[0].Session.Status != session.StatusRunning {
		t.Fatalf("新建终端应立即运行: %+v", views[0])
	}
	if views[0].AutoStart {
		t.Fatal("默认不自动重开")
	}
	if got := ff.Opts()[0].SlotID; got != created.SlotID {
		t.Fatalf("slot 绑定 = %q, want %q", got, created.SlotID)
	}
	if got := ff.Opts()[0].InitialCommand; got != "" {
		t.Fatalf("手动新建不应带初始命令: %q", got)
	}
	if _, ok := st.TerminalByID(created.SlotID); !ok {
		t.Fatal("记忆未落盘")
	}

	// 删除终端 → 记忆删除、shell 终止。
	_, resp = postJSON(t, srv, http.MethodDelete, "/api/v1/terminals/"+created.SlotID, "")
	if !resp.OK {
		t.Fatalf("delete: %+v", resp)
	}
	if left := terminals(t, srv); len(left) != 0 {
		t.Fatalf("删除后 terminals = %+v", left)
	}
	if len(ff.Terms()) != 1 || !ff.Terms()[0].Closed() {
		t.Fatal("删除终端后 shell 应被终止")
	}
}

// TestTerminalIdle_AfterShellExit shell 自退后记忆保留为未启动。
func TestTerminalIdle_AfterShellExit(t *testing.T) {
	srv, ff, _ := newTestServer(t)
	workDir := t.TempDir()

	_, resp := postJSON(t, srv, http.MethodPost, "/api/v1/terminals", dirBody(t, workDir))
	if !resp.OK {
		t.Fatalf("create: %+v", resp)
	}
	slot := terminals(t, srv)[0]

	// shell 自行退出。
	if err := ff.Terms()[0].Close(); err != nil {
		t.Fatal(err)
	}

	views := waitTerminalIdle(t, srv, slot.ID)
	if views.ID != slot.ID {
		t.Fatal("记忆应保留")
	}
	if views.Session != nil {
		t.Fatalf("shell 退出后应未启动: %+v", views.Session)
	}
}

// TestTerminalAutoStartOnlyOnRestore 只有勾选的终端记忆在启动时被拉起。
func TestTerminalAutoStartOnlyOnRestore(t *testing.T) {
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// 两条记忆，其中一条勾选自动重开。
	manual, err := st.AppendTerminal(`C:\work-manual`, "manual")
	if err != nil {
		t.Fatal(err)
	}
	auto, err := st.AppendTerminal(`C:\work-auto`, "manual")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateTerminalAutoStart(auto.ID, true); err != nil {
		t.Fatal(err)
	}
	_ = manual

	// 重新装配（模拟 termhub 重启）。
	srv2 := newServerWithStore(st, &session.FakeFactory{}, nil)
	views := terminals(t, srv2)
	if len(views) != 2 {
		t.Fatalf("terminals = %+v", views)
	}
	started := 0
	for _, view := range views {
		if view.Session != nil {
			started++
		}
	}
	if started != 1 {
		t.Fatalf("只有勾选自动重开的终端应被拉起，实际 %d 个运行中", started)
	}
}

// TestTerminalToggleAutoStart PUT 勾选状态更新并落盘。
func TestTerminalToggleAutoStart(t *testing.T) {
	srv, _, st := newTestServer(t)
	slot, err := st.AppendTerminal(`C:\work`, "manual")
	if err != nil {
		t.Fatal(err)
	}
	_, resp := postJSON(t, srv, http.MethodPut, "/api/v1/terminals/"+slot.ID, `{"auto_start":true}`)
	if !resp.OK {
		t.Fatalf("update: %+v", resp)
	}
	got, ok := st.TerminalByID(slot.ID)
	if !ok || !got.AutoStart {
		t.Fatalf("auto_start = %+v', should be true", got)
	}
	_, resp = postJSON(t, srv, http.MethodPut, "/api/v1/terminals/"+slot.ID, `{"auto_start":false}`)
	if !resp.OK {
		t.Fatalf("update off: %+v", resp)
	}
	got, _ = st.TerminalByID(slot.ID)
	if got.AutoStart {
		t.Fatal("auto_start 未关回")
	}
}

// waitTerminalIdle 轮询直到该终端记忆转为未启动，返回最新视图。
func waitTerminalIdle(t *testing.T, srv *Server, slotID string) terminalView {
	t.Helper()
	for i := 0; i < 200; i++ {
		for _, view := range terminals(t, srv) {
			if view.ID == slotID && view.Session == nil {
				return view
			}
		}
	}
	t.Fatal("等待终端转为未启动超时")
	return terminalView{}
}
