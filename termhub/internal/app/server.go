// Package app 负责装配：本地 HTTP 服务（静态前端 + REST + WS 事件桥）与 Wails 桌面壳。
package app

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"

	"github.com/coder/websocket"

	"github.com/shihao-hub/termhub/internal/pty"
	"github.com/shihao-hub/termhub/internal/session"
	"github.com/shihao-hub/termhub/internal/store"
	"github.com/shihao-hub/termhub/internal/web"
)

// APIResponse 统一响应结构。
type APIResponse struct {
	OK    bool   `json:"ok"`
	Data  any    `json:"data,omitempty"`
	Error string `json:"error,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, resp APIResponse) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(resp)
}

// Server termhub 的本地服务：前端静态资源 + 接缝方法 + WS 输出桥。
type Server struct {
	svc   *session.Service
	store *store.Store

	mu   sync.Mutex
	addr string
}

// NewServer 依据数据目录构造服务。
func NewServer(dataDir string) *Server {
	var st *store.Store
	if dataDir != "" {
		if created, err := store.New(dataDir); err == nil {
			st = created
		}
	}
	svc := session.NewServiceWithStore(pty.ConPtyFactory{}, st)
	return &Server{svc: svc, store: st}
}

// Handler 装配全部路由。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/", web.Handler())
	mux.HandleFunc("GET /api/v1/sessions", s.handleListSessions)
	mux.HandleFunc("POST /api/v1/sessions", s.handleStartSession)
	mux.HandleFunc("DELETE /api/v1/sessions/{id}", s.handleStopSession)
	mux.HandleFunc("GET /api/v1/sessions/history", s.handleSessionHistory)
	mux.HandleFunc("DELETE /api/v1/sessions/history/{id}", s.handleDeleteSessionHistory)
	mux.HandleFunc("POST /api/v1/sessions/{id}/agent", s.handleAgent)
	mux.HandleFunc("GET /api/v1/config", s.handleGetConfig)
	mux.HandleFunc("PUT /api/v1/config", s.handlePutConfig)
	mux.HandleFunc("POST /api/v1/config/entries/{id}/start", s.handleStartEntry)
	mux.HandleFunc("POST /api/v1/config/groups/{id}/start", s.handleStartGroup)
	mux.HandleFunc("GET /api/v1/history/global", s.handleGlobalHistory)
	mux.HandleFunc("GET /api/v1/fs/list", s.handleListDirs)
	mux.HandleFunc("GET /api/v1/ws/{id}", s.handleWS)
	return mux
}

// handleListSessions GET /api/v1/sessions
func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, APIResponse{OK: true, Data: s.svc.ListSessions()})
}

// startRequest POST /api/v1/sessions 请求体。
type startRequest struct {
	WorkDir        string `json:"work_dir"`
	InitialCommand string `json:"initial_command"`
	Source         string `json:"source"`
}

// handleStartSession POST /api/v1/sessions
func (s *Server) handleStartSession(w http.ResponseWriter, r *http.Request) {
	var req startRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	if req.WorkDir != "" {
		info, err := os.Stat(req.WorkDir)
		if err != nil || !info.IsDir() {
			writeJSON(w, http.StatusBadRequest, APIResponse{OK: false, Error: "工作目录不存在: " + req.WorkDir})
			return
		}
	}
	opts := pty.Options{WorkDir: req.WorkDir, InitialCommand: req.InitialCommand, Source: req.Source}
	id, err := s.svc.StartSession(r.Context(), opts)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{OK: false, Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, APIResponse{OK: true, Data: map[string]string{"id": id}})
}

// handleStopSession DELETE /api/v1/sessions/{id}
func (s *Server) handleStopSession(w http.ResponseWriter, r *http.Request) {
	if err := s.svc.StopSession(r.PathValue("id")); err != nil {
		writeJSON(w, http.StatusNotFound, APIResponse{OK: false, Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, APIResponse{OK: true})
}

// handleSessionHistory GET /api/v1/sessions/history
func (s *Server) handleSessionHistory(w http.ResponseWriter, _ *http.Request) {
	if s.store == nil {
		writeJSON(w, http.StatusOK, APIResponse{OK: true, Data: []any{}})
		return
	}
	writeJSON(w, http.StatusOK, APIResponse{OK: true, Data: s.svc.History()})
}

// handleDeleteSessionHistory DELETE /api/v1/sessions/history/{id}
func (s *Server) handleDeleteSessionHistory(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{OK: false, Error: "存储未初始化"})
		return
	}
	if err := s.store.DeleteSession(r.PathValue("id")); err != nil {
		writeJSON(w, http.StatusNotFound, APIResponse{OK: false, Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, APIResponse{OK: true})
}

// agentRequest POST /api/v1/sessions/{id}/agent
type agentRequest struct {
	Question       string `json:"question"`
	ConversationID string `json:"conversation_id"`
}

// handleAgent POST /api/v1/sessions/{id}/agent
func (s *Server) handleAgent(w http.ResponseWriter, r *http.Request) {
	var req agentRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	answer, convo, err := s.svc.AskAgent(r.Context(), r.PathValue("id"), req.Question, req.ConversationID)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, APIResponse{OK: false, Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, APIResponse{OK: true, Data: map[string]string{
		"answer":          answer,
		"conversation_id": convo,
	}})
}

// handleGetConfig GET /api/v1/config
func (s *Server) handleGetConfig(w http.ResponseWriter, _ *http.Request) {
	if s.store == nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{OK: false, Error: "存储未初始化"})
		return
	}
	writeJSON(w, http.StatusOK, APIResponse{OK: true, Data: s.store.Config()})
}

// handlePutConfig PUT /api/v1/config
func (s *Server) handlePutConfig(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{OK: false, Error: "存储未初始化"})
		return
	}
	var cfg store.Config
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{OK: false, Error: "配置格式错误: " + err.Error()})
		return
	}
	cfg = normalizeConfig(cfg)
	for _, entry := range cfg.Entries {
		if entry.WorkDir == "" {
			continue
		}
		info, err := os.Stat(entry.WorkDir)
		if err != nil || !info.IsDir() {
			writeJSON(w, http.StatusBadRequest, APIResponse{OK: false, Error: "启动项目录不存在: " + entry.Name})
			return
		}
	}
	if err := s.store.ReplaceConfig(cfg); err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{OK: false, Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, APIResponse{OK: true, Data: cfg})
}

// handleStartEntry POST /api/v1/config/entries/{id}/start
func (s *Server) handleStartEntry(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{OK: false, Error: "存储未初始化"})
		return
	}
	entry, ok := s.store.Entry(r.PathValue("id"))
	if !ok {
		writeJSON(w, http.StatusNotFound, APIResponse{OK: false, Error: "启动项不存在"})
		return
	}
	id, err := s.svc.StartSession(r.Context(), pty.Options{
		WorkDir:        entry.WorkDir,
		InitialCommand: entry.Command,
		Source:         "entry:" + entry.ID,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{OK: false, Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, APIResponse{OK: true, Data: map[string]string{"id": id}})
}

// handleStartGroup POST /api/v1/config/groups/{id}/start
func (s *Server) handleStartGroup(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{OK: false, Error: "存储未初始化"})
		return
	}
	group, ok := s.store.Group(r.PathValue("id"))
	if !ok {
		writeJSON(w, http.StatusNotFound, APIResponse{OK: false, Error: "启动组不存在"})
		return
	}
	ids := make([]string, 0, len(group.EntryIDs))
	errs := make([]string, 0)
	for _, entryID := range group.EntryIDs {
		entry, found := s.store.Entry(entryID)
		if !found {
			errs = append(errs, "启动项不存在: "+entryID)
			continue
		}
		id, err := s.svc.StartSession(r.Context(), pty.Options{
			WorkDir:        entry.WorkDir,
			InitialCommand: entry.Command,
			Source:         "group:" + group.ID,
		})
		if err != nil {
			errs = append(errs, entry.Name+": "+err.Error())
			continue
		}
		ids = append(ids, id)
	}
	writeJSON(w, http.StatusOK, APIResponse{OK: true, Data: map[string]any{
		"ids":    ids,
		"errors": errs,
	}})
}

// handleGlobalHistory GET /api/v1/history/global
func (s *Server) handleGlobalHistory(w http.ResponseWriter, _ *http.Request) {
	if s.store == nil {
		writeJSON(w, http.StatusOK, APIResponse{OK: true, Data: []string{}})
		return
	}
	writeJSON(w, http.StatusOK, APIResponse{OK: true, Data: s.store.History().Global()})
}

// normalizeConfig 补齐 ID、默认名称并清理空引用。
func normalizeConfig(cfg store.Config) store.Config {
	seenEntries := make(map[string]struct{}, len(cfg.Entries))
	entries := make([]store.Entry, 0, len(cfg.Entries))
	for _, entry := range cfg.Entries {
		if entry.ID == "" {
			entry.ID = store.NewID("e")
		}
		if _, dup := seenEntries[entry.ID]; dup {
			entry.ID = store.NewID("e")
		}
		seenEntries[entry.ID] = struct{}{}
		if entry.Name == "" {
			entry.Name = "未命名启动项"
		}
		if entry.WorkDir == "" {
			if home, err := os.UserHomeDir(); err == nil {
				entry.WorkDir = home
			}
		}
		entries = append(entries, entry)
	}
	groups := make([]store.Group, 0, len(cfg.Groups))
	for _, group := range cfg.Groups {
		if group.ID == "" {
			group.ID = store.NewID("g")
		}
		if group.Name == "" {
			group.Name = "未命名启动组"
		}
		entryIDs := make([]string, 0, len(group.EntryIDs))
		for _, entryID := range group.EntryIDs {
			if _, ok := seenEntries[entryID]; ok {
				entryIDs = append(entryIDs, entryID)
			}
		}
		group.EntryIDs = entryIDs
		groups = append(groups, group)
	}
	cfg.Entries = entries
	cfg.Groups = groups
	return cfg
}

// wsClientMsg WS 上行消息：键盘输入或 resize。
type wsClientMsg struct {
	Type string `json:"type"` // input / resize
	Data string `json:"data,omitempty"`
	Cols int    `json:"cols,omitempty"`
	Rows int    `json:"rows,omitempty"`
}

// handleWS GET /api/v1/ws/{id}：终端输出下行 + 输入上行的事件桥。
func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	backlog, ch, stopped, err := s.svc.Subscribe(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, APIResponse{OK: false, Error: err.Error()})
		return
	}

	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer func() {
		_ = conn.Close(websocket.StatusNormalClosure, "")
	}()

	// 回放最近一屏。
	if len(backlog) > 0 {
		if err := conn.Write(r.Context(), websocket.MessageBinary, backlog); err != nil {
			return
		}
	}

	ctx := r.Context()
	// 读泵：上行输入。
	go func() {
		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			var m wsClientMsg
			if err := json.Unmarshal(data, &m); err != nil {
				// 裸二进制直接当作键盘输入（xterm.js 原生模式兼容）。
				_ = s.svc.WriteInput(id, data)
				continue
			}
			switch m.Type {
			case "input":
				_ = s.svc.WriteInput(id, []byte(m.Data))
			case "resize":
				_ = s.svc.Resize(id, m.Cols, m.Rows)
			}
		}
	}()

	// 写泵 + 会话退出通知。
	for {
		select {
		case chunk, ok := <-ch:
			if !ok {
				return
			}
			if err := conn.Write(ctx, websocket.MessageBinary, chunk); err != nil {
				return
			}
		case <-stopped:
			// 服务端主动关闭 WS，告知前端会话已结束。
			_ = conn.Close(websocket.StatusNormalClosure, "session exited")
			return
		case <-ctx.Done():
			return
		}
	}
}

// Start 监听本地端口并开始服务，返回 http server（供关闭）。
func (s *Server) Start(port int) (*http.Server, string, error) {
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return nil, "", fmt.Errorf("启动 HTTP 监听失败: %w", err)
	}
	s.mu.Lock()
	s.addr = "http://" + listener.Addr().String()
	s.mu.Unlock()

	httpServer := &http.Server{Handler: s.Handler()}
	go func() {
		if err := httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			_ = s.addr
		}
	}()
	return httpServer, s.URL(), nil
}

// URL 当前服务地址。
func (s *Server) URL() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addr
}

// Shutdown 终止全部终端会话（应用退出时由 Wails 关闭钩子调用）。
func (s *Server) Shutdown() {
	s.svc.Shutdown()
}
