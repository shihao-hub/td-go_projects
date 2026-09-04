// Package client 是 UI 侧的 aiquickd 连接器：
// 负责 spawn/杀掉后端进程、请求-应答关联、事件订阅、心跳与懒重启。
// UI 只面对 Call/Subscribe/Shutdown，不碰进程与管道。
package client

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"aiquick/internal/api"
	"aiquick/internal/protocol"
)

// State 连接状态。
type State int

const (
	StateDisconnected State = iota
	StateConnected
)

func (s State) String() string {
	if s == StateConnected {
		return "connected"
	}
	return "disconnected"
}

var (
	// ErrClosed 已 Shutdown，不再使用。
	ErrClosed = errors.New("client closed")
	// ErrBackendDead 后端进程已退出（调用方可稍后重试触发懒重启）。
	ErrBackendDead = errors.New("backend dead")
)

// EventCallback 事件回调。注意：在 client 读循环 goroutine 中执行，
// 回调内不要做耗时操作（UI 侧用 fyne.Do 转发）。
type EventCallback func(ev protocol.Event)

// Client aiquickd 连接客户端，并发安全。
type Client struct {
	path   string
	stderr io.Writer
	env    []string // 子进程环境（nil 继承）

	startMu sync.Mutex // 串行化进程启动/重启

	mu      sync.Mutex
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	pending map[int64]chan protocol.Response
	subs    map[string][]EventCallback
	state   State
	hello   api.HelloResult
	closed  bool
	deadCh  chan struct{} // 当前连接的死亡信号（每次 spawn 换新）
	onState func(State)

	writeMu sync.Mutex
	nextID  atomic.Int64
}

// ResolveBackend 按优先级解析 aiquickd.exe 路径：
// explicit 参数 → 环境变量 AIQUICK_BACKEND → 当前 exe 同目录 → cwd → cwd/bin → cwd/../bin。
func ResolveBackend(explicit string) (string, error) {
	var cands []string
	add := func(p string) {
		if p != "" {
			cands = append(cands, p)
		}
	}
	add(explicit)
	add(os.Getenv("AIQUICK_BACKEND"))
	if exe, err := os.Executable(); err == nil {
		add(filepath.Join(filepath.Dir(exe), "aiquickd.exe"))
	}
	add("aiquickd.exe")
	add(filepath.Join("bin", "aiquickd.exe"))
	add(filepath.Join("..", "bin", "aiquickd.exe"))
	for _, p := range cands {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, nil
		}
	}
	return "", fmt.Errorf("aiquickd.exe not found (tried: %v)", cands)
}

// Start 启动后端进程并完成 hello 握手。
// stderr 接收后端日志；nil 表示丢弃。opts 可注入子进程环境变量（测试隔离用）。
func Start(path string, stderr io.Writer, opts ...Option) (*Client, error) {
	if stderr == nil {
		stderr = io.Discard
	}
	c := &Client{
		path:    path,
		stderr:  stderr,
		pending: make(map[int64]chan protocol.Response),
		subs:    make(map[string][]EventCallback),
		deadCh:  make(chan struct{}),
	}
	for _, o := range opts {
		o(c)
	}
	if err := c.ensureRunning(context.Background()); err != nil {
		return nil, err
	}
	return c, nil
}

// Option 定制 Client。
type Option func(*Client)

// WithEnv 覆盖子进程环境变量（nil 则继承父进程）。
func WithEnv(env []string) Option {
	return func(c *Client) { c.env = env }
}

// SetOnState 注册状态变化回调（断开/重连）。回调在任意 goroutine 执行。
func (c *Client) SetOnState(fn func(State)) {
	c.mu.Lock()
	c.onState = fn
	c.mu.Unlock()
}

// State 返回当前连接状态。
func (c *Client) State() State {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}

// Hello 返回握手信息。
func (c *Client) Hello() api.HelloResult {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hello
}

// Pid 返回当前后端进程 PID；未运行返回 0。
func (c *Client) Pid() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cmd == nil || c.cmd.Process == nil {
		return 0
	}
	return c.cmd.Process.Pid
}

// Subscribe 订阅事件主题，返回取消订阅函数。
func (c *Client) Subscribe(topic string, cb EventCallback) (cancel func()) {
	c.mu.Lock()
	c.subs[topic] = append(c.subs[topic], cb)
	c.mu.Unlock()
	return func() {
		c.mu.Lock()
		list := c.subs[topic]
		for i, f := range list {
			if fmt.Sprintf("%p", f) == fmt.Sprintf("%p", cb) {
				c.subs[topic] = append(list[:i], list[i+1:]...)
				break
			}
		}
		c.mu.Unlock()
	}
}

// Call 发起调用：自动确保后端在运行（掉线则懒重启），
// 阻塞等待应答。result 非 nil 且应答含 result 时反序列化进去。
// 返回请求 rid（可用于事件关联/取消）。
func (c *Client) Call(ctx context.Context, method string, params, result any) (int64, error) {
	return c.CallStream(ctx, method, params, result, nil)
}

// CallStream 与 Call 相同，但请求写出后立即回调 onRID
// （ask.stream 的 chunk 事件早于最终应答到达，UI 需要提前拿到 rid 做过滤）。
func (c *Client) CallStream(ctx context.Context, method string, params, result any, onRID func(rid int64)) (int64, error) {
	if err := c.ensureRunning(ctx); err != nil {
		return 0, err
	}
	raw, err := marshalParams(params)
	if err != nil {
		return 0, err
	}
	id := c.nextID.Add(1)

	ch := make(chan protocol.Response, 1)
	c.mu.Lock()
	c.pending[id] = ch
	c.mu.Unlock()

	line, err := protocol.EncodeLine(protocol.Request{ID: id, Method: method, Params: raw})
	if err != nil {
		c.removePending(id)
		return id, err
	}
	c.writeMu.Lock()
	var werr error
	if c.stdin != nil {
		_, werr = c.stdin.Write(line)
	} else {
		werr = ErrBackendDead
	}
	c.writeMu.Unlock()
	if werr != nil {
		c.removePending(id)
		return id, fmt.Errorf("write request: %w", werr)
	}
	if onRID != nil {
		onRID(id)
	}

	var deadCh chan struct{}
	c.mu.Lock()
	deadCh = c.deadCh
	c.mu.Unlock()

	select {
	case resp, ok := <-ch:
		if !ok {
			return id, ErrBackendDead
		}
		if !resp.OK {
			return id, resp.Error
		}
		if result != nil && len(resp.Result) > 0 {
			if err := jsonUnmarshal(resp.Result, result); err != nil {
				return id, fmt.Errorf("decode result: %w", err)
			}
		}
		return id, nil
	case <-ctx.Done():
		c.removePending(id)
		return id, ctx.Err()
	case <-deadCh:
		c.removePending(id)
		return id, ErrBackendDead
	}
}

// StartHeartbeat 周期 ping：连续 2 次失败强制杀掉后端（触发断开状态）；
// 后端已死时下一次 ping 会自动懒重启（自动恢复）。close 后自动退出。
func (c *Client) StartHeartbeat(interval, timeout time.Duration) {
	go func() {
		fails := 0
		t := time.NewTicker(interval)
		defer t.Stop()
		for range t.C {
			c.mu.Lock()
			closed := c.closed
			c.mu.Unlock()
			if closed {
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			_, err := c.Call(ctx, protocol.MethodPing, nil, nil)
			cancel()
			if err == nil {
				fails = 0
				continue
			}
			fails++
			if fails >= 2 && c.Pid() != 0 {
				_ = c.kill()
				fails = 0
			}
		}
	}()
}

// Shutdown 优雅停止：发 shutdown → 关 stdin → 等待退出（超时 kill）。
// 之后 Client 不可再用（Call 返回 ErrClosed）。
func (c *Client) Shutdown() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	cmd := c.cmd
	c.mu.Unlock()

	if cmd == nil || cmd.Process == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	// 不走 ensureRunning（closed 已置位），直接裸发 shutdown
	_, _ = c.rawRoundtrip(ctx, protocol.MethodShutdown, nil, nil)

	c.writeMu.Lock()
	if c.stdin != nil {
		_ = c.stdin.Close()
	}
	c.writeMu.Unlock()

	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		_ = cmd.Process.Kill()
	}
	c.setState(StateDisconnected)
}

// ---- 内部实现 ----

func (c *Client) ensureRunning(ctx context.Context) error {
	c.mu.Lock()
	closed := c.closed
	running := c.state == StateConnected
	c.mu.Unlock()
	if closed {
		return ErrClosed
	}
	if running {
		return nil
	}

	c.startMu.Lock()
	defer c.startMu.Unlock()
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return ErrClosed
	}
	if c.state == StateConnected { // 双重检查
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()
	return c.spawn(ctx)
}

func (c *Client) spawn(ctx context.Context) error {
	cmd := exec.Command(c.path)
	if c.env != nil {
		cmd.Env = c.env
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = c.stderr
	if attr := procAttr(); attr != nil {
		cmd.SysProcAttr = attr
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start aiquickd (%s): %w", c.path, err)
	}

	c.mu.Lock()
	c.cmd = cmd
	c.stdin = stdin
	c.deadCh = make(chan struct{})
	c.mu.Unlock()

	go c.readLoop(stdout, cmd)

	// 握手
	hctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var h api.HelloResult
	if _, err := c.rawRoundtrip(hctx, protocol.MethodHello, nil, &h); err != nil {
		// 握手失败：杀掉等待下次重试
		_ = cmd.Process.Kill()
		return fmt.Errorf("handshake: %w", err)
	}
	if h.Proto != protocol.Version {
		_ = cmd.Process.Kill()
		return fmt.Errorf("protocol mismatch: backend=%d client=%d", h.Proto, protocol.Version)
	}
	c.mu.Lock()
	c.hello = h
	c.mu.Unlock()
	c.setState(StateConnected)
	return nil
}

// rawRoundtrip 不做 ensureRunning 的裸调用（握手/shutdown 用）。
func (c *Client) rawRoundtrip(ctx context.Context, method string, params, result any) (int64, error) {
	raw, err := marshalParams(params)
	if err != nil {
		return 0, err
	}
	id := c.nextID.Add(1)
	ch := make(chan protocol.Response, 1)
	c.mu.Lock()
	c.pending[id] = ch
	deadCh := c.deadCh
	c.mu.Unlock()

	line, err := protocol.EncodeLine(protocol.Request{ID: id, Method: method, Params: raw})
	if err != nil {
		c.removePending(id)
		return id, err
	}
	c.writeMu.Lock()
	var werr error
	if c.stdin != nil {
		_, werr = c.stdin.Write(line)
	} else {
		werr = ErrBackendDead
	}
	c.writeMu.Unlock()
	if werr != nil {
		c.removePending(id)
		return id, werr
	}

	select {
	case resp, ok := <-ch:
		if !ok {
			return id, ErrBackendDead
		}
		if !resp.OK {
			return id, resp.Error
		}
		if result != nil && len(resp.Result) > 0 {
			if err := jsonUnmarshal(resp.Result, result); err != nil {
				return id, fmt.Errorf("decode result: %w", err)
			}
		}
		return id, nil
	case <-ctx.Done():
		c.removePending(id)
		return id, ctx.Err()
	case <-deadCh:
		c.removePending(id)
		return id, ErrBackendDead
	}
}

func (c *Client) readLoop(r io.Reader, cmd *exec.Cmd) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 8<<20)
	for sc.Scan() {
		env, err := protocol.DecodeEnvelope(sc.Bytes())
		if err != nil {
			continue // 忽略坏行
		}
		if env.IsEvent() {
			c.dispatchEvent(protocol.Event{Event: env.Event, RID: env.RID, Data: env.Data})
			continue
		}
		resp := protocol.Response{ID: env.ID, OK: env.OK != nil && *env.OK, Result: env.Result, Error: env.Error}
		c.deliverResponse(resp)
	}
	c.handleExit()
}

func (c *Client) deliverResponse(resp protocol.Response) {
	c.mu.Lock()
	ch, ok := c.pending[resp.ID]
	if ok {
		delete(c.pending, resp.ID)
	}
	c.mu.Unlock()
	if ok {
		ch <- resp // 缓冲 1，必然成功
	}
}

func (c *Client) dispatchEvent(ev protocol.Event) {
	c.mu.Lock()
	cbs := make([]EventCallback, len(c.subs[ev.Event]))
	copy(cbs, c.subs[ev.Event])
	c.mu.Unlock()
	for _, cb := range cbs {
		cb(ev)
	}
}

func (c *Client) handleExit() {
	c.mu.Lock()
	for id, ch := range c.pending {
		close(ch)
		delete(c.pending, id)
	}
	if c.deadCh != nil {
		close(c.deadCh)
	}
	c.deadCh = nil
	c.stdin = nil
	cmd := c.cmd
	c.cmd = nil
	c.mu.Unlock()

	if cmd != nil && cmd.Process != nil {
		_ = cmd.Wait() // 回收进程
	}
	c.setState(StateDisconnected)
}

func (c *Client) kill() error {
	c.mu.Lock()
	cmd := c.cmd
	c.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}

func (c *Client) setState(s State) {
	c.mu.Lock()
	old := c.state
	c.state = s
	fn := c.onState
	c.mu.Unlock()
	if old != s && fn != nil {
		fn(s)
	}
}

func (c *Client) removePending(id int64) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}
