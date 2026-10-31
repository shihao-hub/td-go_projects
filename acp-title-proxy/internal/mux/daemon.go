package mux

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"sync"
	"time"

	"language_projects/go_projects/acp-title-proxy/internal/config"
	"language_projects/go_projects/acp-title-proxy/internal/finder"
	"language_projects/go_projects/acp-title-proxy/internal/protocol"
	"language_projects/go_projects/acp-title-proxy/internal/skill"
)

// BackendFactory 启动一个后端连接；测试中可注入 mock。
type BackendFactory func(h Handler) (Conn, error)

// Titler 是可选的标题生成器；agy.Titler 满足该接口。
type Titler interface {
	OnSessionNew(sessionID string)
	OnPrompt(sessionID, prompt string)
}

// SkillManager 是可选的技能注入器；skill.Manager 满足该接口。
type SkillManager interface {
	GetCommandItems() []protocol.CommandItem
	MatchAndRewrite(rawPrompt string) (bool, string, string, *skill.Skill)
}

type pendingReq struct {
	client    *Client
	origID    json.RawMessage
	method    string
	capsHash  string
	sessionID string
}

type workerReq struct {
	client   *Client
	workerID json.RawMessage
}

// Daemon 持有唯一的后端 worker，并把 N 个 ACP client 的会话复用到它上面。
type Daemon struct {
	cfg     *config.Config
	logger  *log.Logger
	factory BackendFactory

	ln      net.Listener
	backend Conn

	mu        sync.Mutex
	clients   map[*Client]bool
	sessions  map[string]*Client
	pending   map[string]*pendingReq
	wreq      map[string]*workerReq
	seq       uint64
	clientSeq uint64
	titler    Titler
	skillMgr  SkillManager

	initResult  json.RawMessage
	initCaps    string
	initialized bool

	closed       bool
	shutdownOnce sync.Once
	startedAt    time.Time
}

func NewDaemon(cfg *config.Config, logger *log.Logger, factory BackendFactory) *Daemon {
	if logger == nil {
		logger = log.New(os.Stderr, "[agy-share] ", log.LstdFlags)
	}
	if factory == nil {
		factory = defaultFactory(cfg, logger)
	}
	return &Daemon{
		cfg:      cfg,
		logger:   logger,
		factory:  factory,
		clients:  make(map[*Client]bool),
		sessions: make(map[string]*Client),
		pending:  make(map[string]*pendingReq),
		wreq:     make(map[string]*workerReq),
	}
}

// SetTitler 注入标题生成器（可为 nil）。
func (d *Daemon) SetTitler(t Titler) {
	d.mu.Lock()
	d.titler = t
	d.mu.Unlock()
}

// SetSkillManager 注入技能注入器（可为 nil）。
func (d *Daemon) SetSkillManager(sm SkillManager) {
	d.mu.Lock()
	d.skillMgr = sm
	d.mu.Unlock()
}

func defaultFactory(cfg *config.Config, logger *log.Logger) BackendFactory {
	return func(h Handler) (Conn, error) {
		exe, _, err := finder.FindAGY(cfg.Share.Executable)
		if err != nil {
			return nil, err
		}
		logger.Printf("starting backend: %s", exe)
		return StartBackend(exe, nil, os.Environ(), h)
	}
}

func (d *Daemon) Run() error {
	addr := fmt.Sprintf("127.0.0.1:%d", d.cfg.Share.Port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", addr, err)
	}
	d.ln = ln
	d.startedAt = time.Now()
	d.logger.Printf("listening on %s (max_sessions=%d)", addr, d.cfg.Share.MaxSessions)

	if err := d.startBackend(); err != nil {
		d.logger.Printf("backend start failed: %v", err)
	}

	for {
		conn, err := ln.Accept()
		if err != nil {
			d.mu.Lock()
			closed := d.closed
			d.mu.Unlock()
			if closed {
				return nil
			}
			continue
		}
		go d.handleConn(conn)
	}
}

func (d *Daemon) startBackend() error {
	d.mu.Lock()
	if d.backend != nil {
		d.mu.Unlock()
		return nil
	}
	d.mu.Unlock()

	b, err := d.factory(d)
	if err != nil {
		return err
	}

	d.mu.Lock()
	d.backend = b
	d.mu.Unlock()
	d.logger.Printf("backend ready pid=%d", b.PID())
	return nil
}

func (d *Daemon) handleConn(conn net.Conn) {
	reader := bufio.NewReaderSize(conn, 1<<20)
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	line, err := reader.ReadBytes('\n')
	if err != nil {
		_ = conn.Close()
		return
	}
	_ = conn.SetReadDeadline(time.Time{})

	var hs struct {
		Cmd       string `json:"cmd"`
		Proto     int    `json:"proto"`
		ClientPID int    `json:"clientPid"`
	}
	if err := json.Unmarshal(line, &hs); err != nil {
		writeLine(conn, []byte(`{"status":"error","msg":"invalid handshake"}`))
		_ = conn.Close()
		return
	}

	switch hs.Cmd {
	case "attach":
		writeLine(conn, []byte(`{"status":"ok"}`))
		c := d.newClient(conn, reader)
		d.mu.Lock()
		d.clients[c] = true
		d.mu.Unlock()
		d.logger.Printf("client %s attached (pid=%d)", c.id, hs.ClientPID)
		c.readLoop()

	case "status":
		writeLine(conn, d.statusJSON())
		_ = conn.Close()

	case "stop":
		writeLine(conn, []byte(`{"status":"ok"}`))
		_ = conn.Close()
		// 同步执行：确保 KillTree 在进程退出前完成。
		d.Shutdown()

	default:
		writeLine(conn, []byte(`{"status":"error","msg":"unknown command"}`))
		_ = conn.Close()
	}
}

func writeLine(conn net.Conn, b []byte) {
	_, _ = conn.Write(append(append([]byte(nil), b...), '\n'))
}

func (d *Daemon) newClient(conn net.Conn, r *bufio.Reader) *Client {
	d.mu.Lock()
	d.clientSeq++
	id := "c" + strconv.FormatUint(d.clientSeq, 10)
	d.mu.Unlock()
	return &Client{id: id, conn: conn, reader: r, d: d}
}

// OnLine 实现 Handler。
func (d *Daemon) OnLine(line []byte) {
	m, err := protocol.ParseMessage(line)
	if err != nil {
		d.logger.Printf("backend non-json: %.200s", line)
		return
	}
	switch {
	case m.IsResponse():
		d.fromBackendResponse(m)
	case m.IsRequest():
		d.fromBackendRequest(m)
	case m.IsNotification():
		d.fromBackendNotification(m, line)
	}
}

// OnExit 实现 Handler。
func (d *Daemon) OnExit(err error) {
	d.logger.Printf("backend exited: %v", err)

	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return
	}
	d.backend = nil
	clients := make([]*Client, 0, len(d.clients))
	for c := range d.clients {
		clients = append(clients, c)
	}
	d.clients = make(map[*Client]bool)
	d.sessions = make(map[string]*Client)
	d.pending = make(map[string]*pendingReq)
	d.wreq = make(map[string]*workerReq)
	d.mu.Unlock()

	for _, c := range clients {
		_ = c.conn.Close()
	}
	go d.restartBackend()
}

func (d *Daemon) restartBackend() {
	for i, dly := range []time.Duration{time.Second, 2 * time.Second, 5 * time.Second} {
		time.Sleep(dly)
		d.mu.Lock()
		closed := d.closed
		d.mu.Unlock()
		if closed {
			return
		}
		if err := d.startBackend(); err == nil {
			d.logger.Printf("backend restarted (attempt %d)", i+1)
			return
		} else {
			d.logger.Printf("backend restart attempt %d failed: %v", i+1, err)
		}
	}
	d.logger.Printf("backend restart gave up")
}

func (d *Daemon) fromBackendResponse(m protocol.Message) {
	key := protocol.IDString(m.ID)
	d.mu.Lock()
	pr := d.pending[key]
	if pr != nil {
		delete(d.pending, key)
	}
	d.mu.Unlock()

	if pr == nil {
		d.logger.Printf("response for unknown id %s", key)
		return
	}

	if pr.method == "initialize" && len(m.Error) == 0 {
		d.mu.Lock()
		d.initResult = m.Result
		d.initCaps = pr.capsHash
		d.initialized = true
		d.mu.Unlock()
		d.logger.Printf("initialize cached (caps=%s)", pr.capsHash)
	}

	switch pr.method {
	case "session/new":
		if len(m.Error) == 0 {
			d.mu.Lock()
			sm := d.skillMgr
			d.mu.Unlock()
			if sm != nil {
				if merged, err := protocol.MergeAvailableCommands(m.Result, sm.GetCommandItems()); err == nil {
					m.Result = merged
					m = protocol.StripRaw(m)
				}
			}
			if sid := protocol.ExtractSessionID(m.Result); sid != "" {
				d.mu.Lock()
				d.sessions[sid] = pr.client
				t := d.titler
				d.mu.Unlock()
				if t != nil {
					t.OnSessionNew(sid)
				}
				d.logger.Printf("session %s -> client %s", sid, pr.client.id)
			}
		}
	case "session/load", "session/resume":
		sid := protocol.ExtractSessionID(m.Result)
		if sid == "" {
			sid = pr.sessionID
		}
		if len(m.Error) > 0 {
			if pr.sessionID != "" {
				d.mu.Lock()
				delete(d.sessions, pr.sessionID)
				d.mu.Unlock()
			}
			d.logger.Printf("%s %s failed: %s", pr.method, pr.sessionID, m.Error)
		} else if sid != "" {
			d.mu.Lock()
			d.sessions[sid] = pr.client
			d.mu.Unlock()
			d.logger.Printf("session %s loaded -> client %s", sid, pr.client.id)
		}
	}

	m2 := protocol.WithID(m, pr.origID)
	pr.client.send(protocol.MarshalMessage(m2))
}

func (d *Daemon) fromBackendRequest(m protocol.Message) {
	sid := m.SessionID()

	d.mu.Lock()
	c := d.sessions[sid]
	d.mu.Unlock()

	if c == nil {
		d.logger.Printf("reverse request %s for unknown session %q", m.Method, sid)
		d.sendToBackend(protocol.ErrorResponse(m.ID, -32602, "unknown session"))
		return
	}

	d.mu.Lock()
	d.seq++
	wid := "w" + strconv.FormatUint(d.seq, 10)
	d.wreq[wid] = &workerReq{client: c, workerID: m.ID}
	d.mu.Unlock()

	m2 := protocol.WithID(m, protocol.QuoteID(wid))
	c.send(protocol.MarshalMessage(m2))
}

func (d *Daemon) fromBackendNotification(m protocol.Message, raw []byte) {
	sid := m.SessionID()

	d.mu.Lock()
	c := d.sessions[sid]
	sm := d.skillMgr
	d.mu.Unlock()

	if c == nil {
		d.logger.Printf("notification %s for unknown session %q dropped", m.Method, sid)
		return
	}

	line := raw
	if sm != nil && m.Method == "session/update" {
		if merged, matched, err := protocol.MergeCommandsUpdateNotification(m.Params, sm.GetCommandItems()); err == nil && matched {
			m2 := protocol.StripRaw(m)
			m2.Params = merged
			line = protocol.MarshalMessage(m2)
		}
	}
	c.send(line)
}

func (d *Daemon) fromClient(c *Client, line []byte) {
	m, err := protocol.ParseMessage(line)
	if err != nil {
		d.logger.Printf("client %s non-json: %.200s", c.id, line)
		return
	}
	switch {
	case m.IsRequest():
		d.clientRequest(c, m)
	case m.IsNotification():
		d.clientNotification(m, line)
	case m.IsResponse():
		d.clientResponse(m)
	}
}

func (d *Daemon) clientRequest(c *Client, m protocol.Message) {
	switch m.Method {
	case "initialize":
		d.handleInitialize(c, m)
		return
	case "session/new":
		if !d.allowNewSession(c, m) {
			return
		}
	case "session/load", "session/resume":
		// 历史回放通知可能先于 load 响应到达，必须先建立 session 映射，
		// 否则 replay 的 session/update 会被当作 unknown session 丢弃。
		if sid := m.SessionID(); sid != "" {
			d.mu.Lock()
			d.sessions[sid] = c
			d.mu.Unlock()
			d.logger.Printf("session %s optimistically attached to client %s (%s)", sid, c.id, m.Method)
		}
	case "session/prompt":
		var titleText string
		m, titleText = d.applySkillRewrite(m)
		d.mu.Lock()
		t := d.titler
		d.mu.Unlock()
		if t != nil {
			if sid := m.SessionID(); sid != "" && titleText != "" {
				t.OnPrompt(sid, titleText)
			}
		}
	}
	d.forwardRequest(c, m, m.Method, "")
}

func (d *Daemon) handleInitialize(c *Client, m protocol.Message) {
	caps := protocol.CapabilitiesHash(m.Params)

	d.mu.Lock()
	if d.initialized {
		res := d.initResult
		same := caps == d.initCaps
		allow := d.cfg.Share.AllowMixedCapabilities
		d.mu.Unlock()
		if same || allow {
			c.send(protocol.ResultResponse(m.ID, res))
			return
		}
		c.send(protocol.ErrorResponse(m.ID, -32001,
			"client capabilities differ from the shared worker; restart daemon or enable share.allow_mixed_capabilities"))
		return
	}
	d.mu.Unlock()

	d.forwardRequest(c, m, "initialize", caps)
}

func (d *Daemon) allowNewSession(c *Client, m protocol.Message) bool {
	max := d.cfg.Share.MaxSessions
	if max <= 0 {
		return true
	}
	d.mu.Lock()
	n := len(d.sessions)
	d.mu.Unlock()
	if n < max {
		return true
	}
	c.send(protocol.ErrorResponse(m.ID, -32002, fmt.Sprintf("max_sessions (%d) reached", max)))
	return false
}

func (d *Daemon) forwardRequest(c *Client, m protocol.Message, method, caps string) {
	d.mu.Lock()
	d.seq++
	gid := "d" + strconv.FormatUint(d.seq, 10)
	d.pending[gid] = &pendingReq{client: c, origID: m.ID, method: method, capsHash: caps, sessionID: m.SessionID()}
	b := d.backend
	d.mu.Unlock()

	if b == nil {
		d.mu.Lock()
		delete(d.pending, gid)
		d.mu.Unlock()
		c.send(protocol.ErrorResponse(m.ID, -32000, "agent backend not running"))
		return
	}

	m2 := protocol.WithID(m, protocol.QuoteID(gid))
	if err := b.WriteLine(protocol.MarshalMessage(m2)); err != nil {
		d.mu.Lock()
		delete(d.pending, gid)
		d.mu.Unlock()
		c.send(protocol.ErrorResponse(m.ID, -32000, "failed to write to agent backend: "+err.Error()))
	}
}

func (d *Daemon) clientNotification(m protocol.Message, raw []byte) {
	d.mu.Lock()
	b := d.backend
	d.mu.Unlock()
	if b == nil {
		return
	}
	_ = b.WriteLine(raw)
}

func (d *Daemon) clientResponse(m protocol.Message) {
	key := protocol.IDString(m.ID)

	d.mu.Lock()
	wr := d.wreq[key]
	if wr != nil {
		delete(d.wreq, key)
	}
	b := d.backend
	d.mu.Unlock()

	if wr == nil {
		d.logger.Printf("client response for unknown id %s", key)
		return
	}
	if b == nil {
		return
	}
	m2 := protocol.WithID(m, wr.workerID)
	_ = b.WriteLine(protocol.MarshalMessage(m2))
}

func (d *Daemon) sendToBackend(line []byte) {
	d.mu.Lock()
	b := d.backend
	d.mu.Unlock()
	if b != nil {
		_ = b.WriteLine(line)
	}
}

func (d *Daemon) removeClient(c *Client) {
	d.mu.Lock()
	delete(d.clients, c)

	orphan := make([]string, 0)
	for sid, owner := range d.sessions {
		if owner == c {
			orphan = append(orphan, sid)
			delete(d.sessions, sid)
		}
	}
	for key, pr := range d.pending {
		if pr.client == c {
			delete(d.pending, key)
		}
	}
	for key, wr := range d.wreq {
		if wr.client == c {
			delete(d.wreq, key)
		}
	}
	b := d.backend
	d.mu.Unlock()

	for _, sid := range orphan {
		if b != nil {
			_ = b.WriteLine(protocol.Notification("session/cancel", map[string]any{"sessionId": sid}))
		}
	}
	d.logger.Printf("client %s detached (%d orphan session(s))", c.id, len(orphan))
}

// InjectTitle 实现 agy.Injector：把标题通知推送给拥有该 session 的 client。
func (d *Daemon) InjectTitle(sessionID, title string) error {
	d.mu.Lock()
	c := d.sessions[sessionID]
	d.mu.Unlock()
	if c == nil {
		return fmt.Errorf("unknown session %s", sessionID)
	}
	line := protocol.Notification("session/update", map[string]any{
		"sessionId": sessionID,
		"update": map[string]any{
			"sessionUpdate": "session_info_update",
			"title":         title,
		},
	})
	c.send(line)
	return nil
}

// applySkillRewrite 在 session/prompt 上执行 /skill: 匹配与 prompt 重写；
// 返回可能被重写的消息，以及用于标题生成的干净任务文本。
func (d *Daemon) applySkillRewrite(m protocol.Message) (protocol.Message, string) {
	if len(m.Params) == 0 {
		return m, ""
	}

	var p protocol.SessionPromptParams
	if err := json.Unmarshal(m.Params, &p); err != nil || p.SessionID == "" {
		return m, ""
	}

	text := protocol.ExtractPromptText(p.Prompt)
	if text == "" {
		return m, ""
	}

	d.mu.Lock()
	sm := d.skillMgr
	d.mu.Unlock()
	if sm == nil {
		return m, text
	}

	matched, rewritten, task, _ := sm.MatchAndRewrite(text)
	if !matched {
		return m, text
	}

	titleText := text
	if task != "" {
		titleText = task
	}

	newPrompt, err := protocol.RewritePrompt(p.Prompt, rewritten)
	if err != nil {
		return m, titleText
	}
	p.Prompt = newPrompt
	b, err := json.Marshal(p)
	if err != nil {
		return m, titleText
	}

	m.Params = b
	return protocol.StripRaw(m), titleText
}

func promptText(params json.RawMessage) string {
	var p struct {
		Prompt json.RawMessage `json:"prompt"`
	}
	_ = json.Unmarshal(params, &p)
	return protocol.ExtractPromptText(p.Prompt)
}

func (d *Daemon) statusJSON() []byte {
	d.mu.Lock()
	clients := len(d.clients)
	sessions := len(d.sessions)
	initialized := d.initialized
	b := d.backend
	d.mu.Unlock()

	workerPID := 0
	if b != nil {
		workerPID = b.PID()
	}

	status := map[string]any{
		"status":         "ok",
		"pid":            os.Getpid(),
		"port":           d.cfg.Share.Port,
		"clients":        clients,
		"sessions":       sessions,
		"initialized":    initialized,
		"backend_pid":    workerPID,
		"max_sessions":   d.cfg.Share.MaxSessions,
		"uptime_seconds": int(time.Since(d.startedAt).Seconds()),
	}
	buf, _ := json.Marshal(status)
	return buf
}

func (d *Daemon) Shutdown() {
	// 用 Once 保证 stop 命令与 main 兜底共用同一次清理，
	// 且后到者会阻塞到清理（含 KillTree）完成，避免进程提前退出。
	d.shutdownOnce.Do(func() {
		d.mu.Lock()
		d.closed = true
		ln := d.ln
		clients := make([]*Client, 0, len(d.clients))
		for c := range d.clients {
			clients = append(clients, c)
		}
		d.clients = make(map[*Client]bool)
		b := d.backend
		d.backend = nil
		d.mu.Unlock()

		if ln != nil {
			_ = ln.Close()
		}
		for _, c := range clients {
			_ = c.conn.Close()
		}
		if b != nil {
			_ = b.Close()
		}
		d.logger.Printf("daemon stopped")
	})
}

// Status 供 CLI/测试查询。
func (d *Daemon) Status() map[string]any {
	var v map[string]any
	_ = json.Unmarshal(d.statusJSON(), &v)
	return v
}
