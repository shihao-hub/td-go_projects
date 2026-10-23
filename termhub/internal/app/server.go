// Package app 负责装配：本地 HTTP 服务（静态前端 + REST + WS 事件桥）与 Wails 桌面壳。
package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

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
	log   *slog.Logger

	// OnShow 由桌面壳注入：用户双击 exe 唤出已驻留实例的窗口。
	OnShow func()

	mu   sync.Mutex
	addr string
}

// NewServer 依据数据目录构造服务；已勾选自动重开的终端记忆在此静默拉起。
func NewServer(dataDir string, log *slog.Logger) *Server {
	factory := pty.Factory(pty.ConPtyFactory{})
	return newServerWithFactory(dataDir, factory, log)
}

// newServerWithFactory 测试接缝：注入 fake PTY 工厂验证终端编排。
func newServerWithFactory(dataDir string, factory pty.Factory, log *slog.Logger) *Server {
	var st *store.Store
	if dataDir != "" {
		if created, err := store.New(dataDir); err == nil {
			st = created
		} else if log != nil {
			log.Warn("store init failed", "err", err.Error())
		}
	}
	return newServerWithStore(st, factory, log)
}

// newServerWithStore 测试接缝：直接装配既有存储。
func newServerWithStore(st *store.Store, factory pty.Factory, log *slog.Logger) *Server {
	svc := session.NewServiceWithStore(factory, st)
	s := &Server{svc: svc, store: st, log: log}
	if st != nil {
		s.restoreAutoStart()
	}
	return s
}

// Handler 装配全部路由。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/", web.Handler())
	mux.HandleFunc("GET /api/v1/terminals", s.handleListTerminals)
	mux.HandleFunc("POST /api/v1/terminals", s.handleCreateTerminal)
	mux.HandleFunc("POST /api/v1/terminals/{id}/start", s.handleStartTerminal)
	mux.HandleFunc("PUT /api/v1/terminals/{id}", s.handleUpdateTerminal)
	mux.HandleFunc("DELETE /api/v1/terminals/{id}", s.handleDeleteTerminal)
	mux.HandleFunc("POST /api/v1/sessions/{id}/agent", s.handleAgent)
	mux.HandleFunc("GET /api/v1/config", s.handleGetConfig)
	mux.HandleFunc("PUT /api/v1/config", s.handlePutConfig)
	mux.HandleFunc("POST /api/v1/config/entries/{id}/start", s.handleStartEntry)
	mux.HandleFunc("POST /api/v1/config/groups/{id}/start", s.handleStartGroup)
	mux.HandleFunc("GET /api/v1/history/global", s.handleGlobalHistory)
	mux.HandleFunc("GET /api/v1/fs/list", s.handleListDirs)
	mux.HandleFunc("GET /api/v1/ws/{id}", s.handleWS)
	mux.HandleFunc("GET /api/v1/alive", s.handleAlive)
	mux.HandleFunc("POST /api/v1/ui/show", s.handleShow)
	return mux
}

// handleAlive GET /api/v1/alive：自研单实例的存活探测端点。
func (s *Server) handleAlive(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, APIResponse{OK: true})
}

// handleShow POST /api/v1/ui/show：第二实例触发已驻留实例唤出主窗口。
func (s *Server) handleShow(w http.ResponseWriter, _ *http.Request) {
	if s.OnShow != nil {
		go s.OnShow()
	}
	writeJSON(w, http.StatusOK, APIResponse{OK: true})
}

// terminalView 终端记忆与其运行时状态合并视图。
type terminalView struct {
	ID            string    `json:"id"`
	WorkDir       string    `json:"work_dir"`
	Source        string    `json:"source,omitempty"`
	AutoStart     bool      `json:"auto_start"`
	CreatedAt     time.Time `json:"created_at"`
	LastSessionID string    `json:"last_session_id,omitempty"`
	// LastCommands 最近一次（已结束）shell 运行的命令记录。
	LastCommands []string `json:"last_commands"`
	// Session 当前运行中的 shell 快照；未启动为 nil。
	Session *session.Snapshot `json:"session"`
}

// handleListTerminals GET /api/v1/terminals：按创建顺序返回终端记忆。
func (s *Server) handleListTerminals(w http.ResponseWriter, _ *http.Request) {
	views, err := s.buildTerminalViews()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{OK: false, Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, APIResponse{OK: true, Data: views})
}

func (s *Server) buildTerminalViews() ([]terminalView, error) {
	if s.store == nil {
		return nil, fmt.Errorf("存储未初始化")
	}
	liveBySlot := make(map[string]session.Snapshot)
	for _, snap := range s.svc.ListSessions() {
		if snap.SlotID != "" {
			liveBySlot[snap.SlotID] = snap
		}
	}
	views := make([]terminalView, 0)
	for _, slot := range s.store.Terminals() {
		view := terminalView{
			ID:            slot.ID,
			WorkDir:       slot.WorkDir,
			Source:        slot.Source,
			AutoStart:     slot.AutoStart,
			CreatedAt:     slot.CreatedAt,
			LastSessionID: slot.LastSessionID,
			LastCommands:  []string{},
		}
		if slot.LastSessionID != "" {
			if live, ok := liveBySlot[slot.LastSessionID]; ok {
				liveView := live
				view.Session = &liveView
			} else if rec, ok := s.store.Session(slot.LastSessionID); ok {
				view.LastCommands = rec.Commands
			}
		}
		// 兜底：slot 有正在运行的会话但 LastSessionID 绑定异常时仍显示运行态。
		if view.Session == nil {
			if live, ok := liveBySlot[slot.ID]; ok {
				liveView := live
				view.Session = &liveView
			}
		}
		views = append(views, view)
	}
	return views, nil
}

// handleCreateTerminal POST /api/v1/terminals：新建终端记忆并拉起 shell。
func (s *Server) handleCreateTerminal(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{OK: false, Error: "存储未初始化"})
		return
	}
	var req struct {
		WorkDir        string `json:"work_dir"`
		InitialCommand string `json:"initial_command"`
		Source         string `json:"source"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	if info, err := os.Stat(req.WorkDir); err != nil || !info.IsDir() {
		writeJSON(w, http.StatusBadRequest, APIResponse{OK: false, Error: "工作目录不存在: " + req.WorkDir})
		return
	}
	slot, sessionID, err := s.createAndStartSlot(req.WorkDir, req.Source, req.InitialCommand)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{OK: false, Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, APIResponse{OK: true, Data: map[string]string{
		"slot_id":    slot.ID,
		"session_id": sessionID,
	}})
}

// handleStartTerminal POST /api/v1/terminals/{id}/start：把未启动的终端记忆拉起一个纯净 shell。
func (s *Server) handleStartTerminal(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{OK: false, Error: "存储未初始化"})
		return
	}
	slot, ok := s.store.TerminalByID(r.PathValue("id"))
	if !ok {
		writeJSON(w, http.StatusNotFound, APIResponse{OK: false, Error: "终端记录不存在"})
		return
	}
	if live, ok := s.liveSessionBySlot(slot.ID); ok {
		writeJSON(w, http.StatusOK, APIResponse{OK: true, Data: map[string]string{
			"slot_id":    slot.ID,
			"session_id": live.ID,
		}})
		return
	}
	sessionID, err := s.startSessionForSlot(slot, "")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{OK: false, Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, APIResponse{OK: true, Data: map[string]string{
		"slot_id":    slot.ID,
		"session_id": sessionID,
	}})
}

// handleUpdateTerminal PUT /api/v1/terminals/{id}：更新自动重开勾选。
func (s *Server) handleUpdateTerminal(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{OK: false, Error: "存储未初始化"})
		return
	}
	var req struct {
		AutoStart bool `json:"auto_start"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if err := s.store.UpdateTerminalAutoStart(r.PathValue("id"), req.AutoStart); err != nil {
		writeJSON(w, http.StatusNotFound, APIResponse{OK: false, Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, APIResponse{OK: true})
}

// handleDeleteTerminal DELETE /api/v1/terminals/{id}：
// 用户侧「删除终端」——终止运行中的 shell（若有），移除记忆与最近一次运行的历史文件。
func (s *Server) handleDeleteTerminal(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{OK: false, Error: "存储未初始化"})
		return
	}
	slot, ok := s.store.TerminalByID(r.PathValue("id"))
	if !ok {
		writeJSON(w, http.StatusNotFound, APIResponse{OK: false, Error: "终端记录不存在"})
		return
	}
	if live, found := s.liveSessionBySlot(slot.ID); found {
		_ = s.svc.StopSession(live.ID)
	}
	if err := s.store.DeleteTerminal(slot.ID); err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{OK: false, Error: err.Error()})
		return
	}
	if slot.LastSessionID != "" {
		s.store.History().Cleanup(slot.LastSessionID)
		_ = s.store.DeleteSession(slot.LastSessionID)
	}
	writeJSON(w, http.StatusOK, APIResponse{OK: true})
}

// createAndStartSlot 新建终端记忆并拉起 shell；所有启动入口的统一编排。
// 启动失败撤销新建的记忆，避免留下空槽。
func (s *Server) createAndStartSlot(workDir, source, initialCommand string) (store.TerminalSlot, string, error) {
	slot, err := s.store.AppendTerminal(workDir, source)
	if err != nil {
		return store.TerminalSlot{}, "", err
	}
	sessionID, err := s.startSessionForSlot(slot, initialCommand)
	if err != nil {
		_ = s.store.DeleteTerminal(slot.ID)
		return store.TerminalSlot{}, "", err
	}
	return slot, sessionID, nil
}

// startSessionForSlot 为既有终端记忆拉起一个 shell（不带命令即纯净 PowerShell）。
// 若该记忆最近一次运行已结束，则把其最近一屏带入新会话作为前置回放。
func (s *Server) startSessionForSlot(slot store.TerminalSlot, initialCommand string) (string, error) {
	var priorScreen []byte
	if slot.LastSessionID != "" && s.store != nil {
		priorScreen = s.store.History().ReadSessionScreen(slot.LastSessionID)
	}
	sessionID, err := s.svc.StartSession(context.Background(), pty.Options{
		WorkDir:        slot.WorkDir,
		InitialCommand: initialCommand,
		Source:         slot.Source,
		SlotID:         slot.ID,
		PriorScreen:    priorScreen,
	})
	if err != nil {
		return "", err
	}
	if s.store != nil {
		_ = s.store.UpdateTerminalLastSession(slot.ID, sessionID)
	}
	return sessionID, nil
}

// liveSessionBySlot 查找该终端记忆当前运行中的会话。
func (s *Server) liveSessionBySlot(slotID string) (*session.Snapshot, bool) {
	for _, snap := range s.svc.ListSessions() {
		if snap.SlotID == slotID {
			live := snap
			return &live, true
		}
	}
	return nil, false
}

// restoreAutoStart 启动导流：勾选自动重开的终端记忆逐一拉起纯净 shell。
// 拉起就是干净的 PowerShell，不注入任何初始命令。
func (s *Server) restoreAutoStart() {
	restored := 0
	for _, slot := range s.store.Terminals() {
		if !slot.AutoStart {
			continue
		}
		if _, err := s.startSessionForSlot(slot, ""); err != nil {
			if s.log != nil {
				s.log.Warn("autostart restore failed", "slot", slot.ID, "err", err.Error())
			}
			continue
		}
		restored++
	}
	if restored > 0 && s.log != nil {
		s.log.Info("autostart restored terminals", "count", restored)
	}
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
// 启动项作为终端记忆落入左侧列表再拉起 shell，与手动新建走同一模型。
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
	slot, sessionID, err := s.createAndStartSlot(entry.WorkDir, "entry:"+entry.ID, entry.Command)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{OK: false, Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, APIResponse{OK: true, Data: map[string]string{
		"slot_id": slot.ID, "session_id": sessionID,
	}})
}

// handleStartGroup POST /api/v1/config/groups/{id}/start
// 组内每个启动项都落成一条终端记忆再拉起，与统一终端模型一致。
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
	slots := make([]string, 0, len(group.EntryIDs))
	errs := make([]string, 0)
	for _, entryID := range group.EntryIDs {
		entry, found := s.store.Entry(entryID)
		if !found {
			errs = append(errs, "启动项不存在: "+entryID)
			continue
		}
		slot, sessionID, err := s.createAndStartSlot(entry.WorkDir, "group:"+group.ID, entry.Command)
		if err != nil {
			errs = append(errs, entry.Name+": "+err.Error())
			continue
		}
		slots = append(slots, slot.ID)
		ids = append(ids, sessionID)
	}
	writeJSON(w, http.StatusOK, APIResponse{OK: true, Data: map[string]any{
		"slot_ids": slots,
		"ids":      ids,
		"errors":   errs,
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
