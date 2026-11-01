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
	"language_projects/go_projects/acp-title-proxy/internal/pool"
	"language_projects/go_projects/acp-title-proxy/internal/protocol"
	"language_projects/go_projects/acp-title-proxy/internal/skill"
	"language_projects/go_projects/acp-title-proxy/internal/updater"
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

// sessionTableHardCap 是路由表条数的硬上限，防止单个 client 长期在线时
// 无限累积历史会话映射。它只兜底，不参与正常配额判断。
const sessionTableHardCap = 256

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

	// sessionActivity 记录每个会话最近一次消息往返时间，用于区分
	// 「正在干活的活跃会话」与「只是被 load 出来看着的历史会话」。
	sessionActivity map[string]time.Time
	// replaying 标记正在回放历史的会话；回放期间的反向消息不计入活跃，
	// 否则「打开旧对话看一眼」又会把配额吃满。
	replaying map[string]bool

	initResult  json.RawMessage
	initCaps    string
	initialized bool

	closed       bool
	shutdownOnce sync.Once
	stopCh       chan struct{}
	lastClientAt time.Time
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
		cfg:             cfg,
		logger:          logger,
		factory:         factory,
		clients:         make(map[*Client]bool),
		sessions:        make(map[string]*Client),
		pending:         make(map[string]*pendingReq),
		wreq:            make(map[string]*workerReq),
		sessionActivity: make(map[string]time.Time),
		replaying:       make(map[string]bool),
		stopCh:          make(chan struct{}),
		lastClientAt:    time.Now(),
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
	d.logger.Printf("listening on %s (max_sessions=%d active, window=%s)", addr, d.cfg.Share.MaxSessions, d.activeWindow())

	if err := d.startBackend(); err != nil {
		d.logger.Printf("backend start failed: %v", err)
	}
	go d.runIdleExit()
	go d.runOrphanWatch()

	if d.cfg.AutoUpdate.Enabled {
		interval := time.Duration(d.cfg.AutoUpdate.CheckIntervalHours) * time.Hour
		updater.StartBackgroundLoop(d.logger, interval, d.stopCh)
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
		d.lastClientAt = time.Now()
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
	d.sessionActivity = make(map[string]time.Time)
	d.replaying = make(map[string]bool)
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

// runIdleExit 在所有 client 断开且空闲超过 share.idle_ttl_minutes 后退出 daemon，
// 释放 worker 及其全部 localharness。下次 panel 打开时 serve 会自动拉起。
func (d *Daemon) runIdleExit() {
	ttl := time.Duration(d.cfg.Share.IdleTTLMinutes) * time.Minute
	if ttl <= 0 {
		return
	}
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-d.stopCh:
			return
		case <-ticker.C:
			d.mu.Lock()
			clients := len(d.clients)
			last := d.lastClientAt
			d.mu.Unlock()
			if clients == 0 && time.Since(last) >= ttl {
				d.logger.Printf("share daemon idle for %s (no clients), shutting down", time.Since(last).Round(time.Second))
				d.Shutdown()
				return
			}
		}
	}
}

// runOrphanWatch 定期清理父进程已退出的 AGY 孤儿进程。
// 背景：Zed 每打开一个 AGY 会话就会拉起 agy_acp_server（约 12MB）与其 localharness_external
// 子进程（120MB 以上）；客户端退出后这两个进程在 Windows 上不会自行退出，
// 反复切换会话会把内存推到十几 GB。父进程仍存活的进程一律不动，因此不会影响活跃会话。
func (d *Daemon) runOrphanWatch() {
	if d.cfg == nil || !d.cfg.Reaper.Enabled {
		return
	}
	interval := time.Duration(d.cfg.Reaper.CheckIntervalMinutes) * time.Minute
	if interval < time.Minute {
		interval = time.Minute
	}

	runOnce := func() {
		res := pool.ReapOrphans(false)
		res.Logf(d.logger)
	}

	select {
	case <-d.stopCh:
		return
	case <-time.After(30 * time.Second):
		runOnce()
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-d.stopCh:
			return
		case <-ticker.C:
			runOnce()
		}
	}
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
				d.touchSessionLocked(sid)
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
				delete(d.sessionActivity, pr.sessionID)
				delete(d.replaying, pr.sessionID)
				d.mu.Unlock()
			}
			d.logger.Printf("%s %s failed: %s", pr.method, pr.sessionID, m.Error)
		} else if sid != "" {
			d.mu.Lock()
			d.sessions[sid] = pr.client
			// 回放结束：仍然不算活跃，等用户真正发 prompt 再计时。
			delete(d.replaying, sid)
			d.mu.Unlock()
			d.logger.Printf("session %s loaded -> client %s (idle until prompted)", sid, pr.client.id)
		}
	}

	m2 := protocol.WithID(m, pr.origID)
	pr.client.send(protocol.MarshalMessage(m2))
}

func (d *Daemon) fromBackendRequest(m protocol.Message) {
	sid := m.SessionID()

	d.mu.Lock()
	c := d.sessions[sid]
	if c != nil && !d.replaying[sid] {
		d.touchSessionLocked(sid)
	}
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
	if c != nil && !d.replaying[sid] {
		d.touchSessionLocked(sid)
	}
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
	// session/load 只是打开历史，不算活跃；其余带 sessionId 的消息都会刷新活跃时间。
	if m.Method != "session/load" && m.Method != "session/resume" {
		d.markSessionActive(m.SessionID())
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

// markSessionActive 只对已挂载的会话刷新活跃时间，
// 避免未知 sessionId 在活动表里留下垃圾条目。
func (d *Daemon) markSessionActive(sid string) {
	if sid == "" {
		return
	}
	d.mu.Lock()
	if _, ok := d.sessions[sid]; ok {
		d.touchSessionLocked(sid)
	}
	d.mu.Unlock()
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
			// 只是「打开来看」，不刷新活跃时间；回放期间的消息也不计入配额，
			// 等用户真正发 prompt 时才算这个会话开始干活。
			d.replaying[sid] = true
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

// touchSessionLocked 记录会话最近一次消息往返时间；调用方需持有 d.mu。
func (d *Daemon) touchSessionLocked(sid string) {
	if sid == "" {
		return
	}
	d.sessionActivity[sid] = time.Now()
}

// activeWindow 返回判定会话活跃的时间窗口。
func (d *Daemon) activeWindow() time.Duration {
	minutes := d.cfg.Share.ActiveWindowMinutes
	if minutes <= 0 {
		minutes = 15
	}
	return time.Duration(minutes) * time.Minute
}

// allowNewSession 只按「活跃会话」判定配额。
//
// 历史会话（用户只是 load 出来看）超过 activeWindow 后不再计数，
// 因此翻看旧对话不会把配额永久吃满；它们仍留在路由表里，
// 反向请求照常可达，只是不再占用新建名额。
func (d *Daemon) allowNewSession(c *Client, m protocol.Message) bool {
	max := d.cfg.Share.MaxSessions
	if max <= 0 {
		return true
	}
	window := d.activeWindow()

	d.mu.Lock()
	now := time.Now()
	active := 0
	for sid := range d.sessions {
		at, ok := d.sessionActivity[sid]
		if !ok {
			// 没有活动记录的会话（理论上不存在）按非活跃处理，
			// 避免旧数据永久占位。
			continue
		}
		if now.Sub(at) <= window {
			active++
		} else {
			// 顺手清理过期的活动时间，防止 map 随历史会话无限增长。
			delete(d.sessionActivity, sid)
		}
	}
	total := len(d.sessions)
	d.mu.Unlock()

	if total >= sessionTableHardCap {
		c.send(protocol.ErrorResponse(m.ID, -32002, fmt.Sprintf(
			"session table full (%d entries); restart the share daemon with `acp-title-proxy share-restart`",
			sessionTableHardCap)))
		return false
	}
	if active < max {
		return true
	}

	d.logger.Printf("reject session/new: %d active session(s) in last %s (max %d), table has %d entr(ies)",
		active, window, max, total)
	c.send(protocol.ErrorResponse(m.ID, -32002, fmt.Sprintf(
		"max_sessions (%d) reached: %d session(s) active within %s; %d historical session(s) loaded but idle",
		max, active, window, total-active)))
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
	d.lastClientAt = time.Now()

	orphan := make([]string, 0)
	for sid, owner := range d.sessions {
		if owner == c {
			orphan = append(orphan, sid)
			delete(d.sessions, sid)
			delete(d.sessionActivity, sid)
			delete(d.replaying, sid)
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
	window := d.activeWindow()
	now := time.Now()

	d.mu.Lock()
	clients := len(d.clients)
	sessions := len(d.sessions)
	active := 0
	for sid := range d.sessions {
		if at, ok := d.sessionActivity[sid]; ok && now.Sub(at) <= window {
			active++
		}
	}
	initialized := d.initialized
	b := d.backend
	d.mu.Unlock()

	workerPID := 0
	if b != nil {
		workerPID = b.PID()
	}

	status := map[string]any{
		"status":                "ok",
		"pid":                   os.Getpid(),
		"port":                  d.cfg.Share.Port,
		"clients":               clients,
		"sessions":              sessions,
		"active_sessions":       active,
		"initialized":           initialized,
		"backend_pid":           workerPID,
		"max_sessions":          d.cfg.Share.MaxSessions,
		"active_window_minutes": int(window.Minutes()),
		"uptime_seconds":        int(time.Since(d.startedAt).Seconds()),
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
		close(d.stopCh)
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
