package pool

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"language_projects/go_projects/acp-title-proxy/internal/config"
	"language_projects/go_projects/acp-title-proxy/internal/finder"
)

type Daemon struct {
	cfg            *config.Config
	port           int
	warmupDuration time.Duration
	exeModTime     int64

	listener    net.Listener
	standbyChan chan *Worker

	mu          sync.Mutex
	closed      bool
	workers     map[*Worker]bool
	replenishCh chan struct{}
}

func NewDaemon(cfg *config.Config, port int, warmupDuration time.Duration) *Daemon {
	if port <= 0 {
		port = DefaultPort
	}
	if warmupDuration <= 0 {
		warmupDuration = 8 * time.Second
	}

	return &Daemon{
		cfg:            cfg,
		port:           port,
		warmupDuration: warmupDuration,
		exeModTime:     GetSelfModTime(),
		standbyChan:    make(chan *Worker, 1),
		workers:        make(map[*Worker]bool),
		replenishCh:    make(chan struct{}, 1),
	}
}

func (d *Daemon) Start(stopCh <-chan struct{}) error {
	addr := fmt.Sprintf("127.0.0.1:%d", d.port)
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}
	d.listener = l
	fmt.Printf("[Pre-warm Daemon] Listening on %s\n", addr)

	// 启动后台补充循环
	go d.replenishLoop(stopCh)
	d.triggerReplenish()

	// 启动闲置沙箱回收巡检循环
	if d.cfg != nil && d.cfg.Reaper.Enabled {
		reaper := NewReaper(d.cfg.Reaper, d.getWorkerPids)
		go reaper.Start(stopCh)
		fmt.Printf("[Pre-warm Daemon] Idle harness reaper started (IdleTimeout: %dm, CheckInterval: %dm)\n",
			d.cfg.Reaper.IdleMinutes, d.cfg.Reaper.CheckIntervalMinutes)
	}

	// 接受连接循环
	go func() {
		<-stopCh
		d.mu.Lock()
		d.closed = true
		if d.listener != nil {
			_ = d.listener.Close()
		}
		// 终止所有剩余 worker
		for w := range d.workers {
			_ = w.Close()
		}
		d.mu.Unlock()
	}()

	for {
		conn, err := d.listener.Accept()
		if err != nil {
			d.mu.Lock()
			closed := d.closed
			d.mu.Unlock()
			if closed {
				return nil
			}
			continue
		}

		go d.handleConnection(conn)
	}
}

func (d *Daemon) triggerReplenish() {
	select {
	case d.replenishCh <- struct{}{}:
	default:
	}
}

func (d *Daemon) replenishLoop(stopCh <-chan struct{}) {
	for {
		select {
		case <-stopCh:
			return
		case <-d.replenishCh:
			d.mu.Lock()
			if d.closed {
				d.mu.Unlock()
				return
			}
			d.mu.Unlock()

			// 检查是否已有待命 worker
			if len(d.standbyChan) > 0 {
				continue
			}

			// 预热一个新 worker
			w, err := d.createAndWarmupWorker(stopCh)
			if err != nil {
				fmt.Fprintf(os.Stderr, "[Pre-warm Daemon] Failed to pre-warm worker: %v\n", err)
				time.Sleep(2 * time.Second)
				d.triggerReplenish()
				continue
			}

			if w != nil {
				d.mu.Lock()
				d.workers[w] = true
				d.mu.Unlock()

				d.standbyChan <- w
				fmt.Printf("[Pre-warm Daemon] Standby AGY worker ready (PID: %d)\n", w.Cmd.Process.Pid)
			}
		}
	}
}

func (d *Daemon) createAndWarmupWorker(stopCh <-chan struct{}) (*Worker, error) {
	exePath, args, err := finder.FindAGY(d.cfg.AGY.Executable)
	if err != nil {
		return nil, err
	}

	fmt.Printf("[Pre-warm Daemon] Starting new AGY instance for pre-warming: %s\n", exePath)

	// 自动向预热的 worker 注入代理与 Zed CA 证书
	env := os.Environ()
	ensureEnv(&env, "HTTP_PROXY", "http://127.0.0.1:7897")
	ensureEnv(&env, "HTTPS_PROXY", "http://127.0.0.1:7897")
	ensureEnv(&env, "NO_PROXY", "localhost,127.0.0.1,::1")
	if appData := os.Getenv("APPDATA"); appData != "" {
		caFile := filepath.Join(appData, "Zed", "certs", "antigravity-ca.pem")
		if _, err := os.Stat(caFile); err == nil {
			ensureEnv(&env, "REQUESTS_CA_BUNDLE", caFile)
			ensureEnv(&env, "SSL_CERT_FILE", caFile)
		}
	}

	w, err := StartWorker(exePath, args, env)
	if err != nil {
		return nil, err
	}

	// 在后台主动完成 ACP initialize 握手，
	// 强制 AGY 的 Python 引擎预先导入模块、解析鉴权并缓存
	// 能力（后台约需 6 秒，前端命中时耗时 0ms）。
	initReq := []byte("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\",\"params\":{\"protocolVersion\":1,\"clientInfo\":{\"name\":\"Daemon-Prewarm\",\"version\":\"1.0\"}}}\n")
	if _, err := w.Stdin.Write(initReq); err != nil {
		_ = w.Close()
		return nil, fmt.Errorf("failed to send initialize request: %w", err)
	}

	readDone := make(chan error, 1)
	go func() {
		line, err := w.StdoutReader.ReadBytes('\n')
		if err != nil {
			readDone <- err
			return
		}
		if len(line) == 0 {
			readDone <- io.EOF
			return
		}
		readDone <- nil
	}()

	select {
	case <-stopCh:
		_ = w.Close()
		return nil, fmt.Errorf("daemon stopping")
	case waitErr := <-w.WaitErr:
		_ = w.Close()
		return nil, fmt.Errorf("worker died during warmup: %v", waitErr)
	case err := <-readDone:
		if err != nil {
			_ = w.Close()
			return nil, fmt.Errorf("worker initialize failed: %w", err)
		}
		// 预热成功，worker 已就绪！
		return w, nil
	case <-time.After(25 * time.Second):
		_ = w.Close()
		return nil, fmt.Errorf("worker warmup timed out after 25s")
	}
}

func (d *Daemon) handleConnection(conn net.Conn) {
	defer conn.Close()

	reader := bufio.NewReader(conn)
	line, err := reader.ReadBytes('\n')
	if err != nil {
		return
	}

	var req PoolRequest
	if err := json.Unmarshal(line, &req); err != nil {
		_ = sendResponse(conn, PoolResponse{Status: "error", Msg: "invalid_json"})
		return
	}

	switch req.Cmd {
	case "ping":
		_ = sendResponse(conn, PoolResponse{Status: "ok", Msg: "pong"})

	case "stop":
		_ = sendResponse(conn, PoolResponse{Status: "ok", Msg: "stopping"})
		fmt.Println("[Pre-warm Daemon] Stop command received. Shutting down...")
		go func() {
			time.Sleep(100 * time.Millisecond)
			d.shutdown()
		}()
		return

	case "status":
		d.mu.Lock()
		count := len(d.standbyChan)
		d.mu.Unlock()
		_ = sendResponse(conn, PoolResponse{
			Status:  "ok",
			Msg:     fmt.Sprintf("standby_count=%d", count),
			Version: d.exeModTime,
		})

	case "acquire":
		// 版本自愈检测：检查客户端可执行文件是否已更新
		if req.ExeModTime > 0 && d.exeModTime > 0 && req.ExeModTime != d.exeModTime {
			fmt.Printf("[Pre-warm Daemon] Detected updated proxy binary (client: %d, daemon: %d). Retiring outdated daemon.\n", req.ExeModTime, d.exeModTime)
			_ = sendResponse(conn, PoolResponse{
				Status:  "outdated",
				Msg:     "daemon_outdated",
				Version: d.exeModTime,
			})
			go func() {
				time.Sleep(100 * time.Millisecond)
				d.shutdown()
			}()
			return
		}

		// 尝试获取待命 worker
		var worker *Worker
		select {
		case worker = <-d.standbyChan:
		case <-time.After(500 * time.Millisecond):
			// 当前没有可用的待命 worker
		}

		if worker != nil {
			// 自动升级检测：检查 Zed 是否已下载更新的 AGY 版本
			if latestExe, _, err := finder.FindAGY(d.cfg.AGY.Executable); err == nil && latestExe != "" && worker.ExePath != latestExe {
				fmt.Printf("[Pre-warm Daemon] Detected new AGY version: %s (was %s). Discarding outdated worker.\n", latestExe, worker.ExePath)
				_ = worker.Close()
				d.removeWorker(worker)
				worker = nil
			}
		}

		if worker == nil {
			_ = sendResponse(conn, PoolResponse{Status: "error", Msg: "no_standby_worker"})
			d.triggerReplenish()
			return
		}

		// 成功，通知客户端
		if err := sendResponse(conn, PoolResponse{Status: "ok", Msg: "worker_assigned"}); err != nil {
			_ = worker.Close()
			d.removeWorker(worker)
			d.triggerReplenish()
			return
		}

		// 触发后台补充，为下一个会话做准备
		d.triggerReplenish()

		// 在 TCP 连接与 worker 进程之间双向转发数据
		d.splice(conn, reader, worker)
		d.removeWorker(worker)

	default:
		_ = sendResponse(conn, PoolResponse{Status: "error", Msg: "unknown_command"})
	}
}

func (d *Daemon) splice(conn net.Conn, reader io.Reader, worker *Worker) {
	defer worker.Close()
	defer conn.Close()

	done := make(chan struct{})
	var closeOnce sync.Once
	abort := func() {
		closeOnce.Do(func() {
			close(done)
			_ = conn.Close()
			_ = worker.Close()
		})
	}

	var wg sync.WaitGroup
	wg.Add(2)

	// 入向：客户端（经 reader）-> worker.Stdin
	go func() {
		defer wg.Done()
		defer func() {
			if worker.Stdin != nil {
				_ = worker.Stdin.Close()
			}
			abort() // 客户端断开（EOF 或错误），立即中止
		}()

		// 直接从 reader 拷贝，已预缓冲的字节会被自动带走
		_, _ = io.Copy(worker.Stdin, touchReader{r: reader, w: worker})
	}()

	// 出向：worker.Stdout（经 StdoutReader）-> 客户端连接
	go func() {
		defer wg.Done()
		defer abort()

		_, _ = io.Copy(touchWriter{w: conn, track: worker}, worker.StdoutReader)
	}()

	// 等待任一方向结束
	select {
	case <-done:
	}

	// 若 worker 未正常退出则强制终止
	killTimer := time.AfterFunc(300*time.Millisecond, func() {
		_ = worker.Close()
	})
	wg.Wait()
	killTimer.Stop()
}

func ensureEnv(env *[]string, key, fallbackVal string) {
	prefix := strings.ToUpper(key) + "="
	for _, e := range *env {
		if strings.HasPrefix(strings.ToUpper(e), prefix) {
			return
		}
	}
	*env = append(*env, key+"="+fallbackVal)
}

func (d *Daemon) shutdown() {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return
	}
	d.closed = true
	if d.listener != nil {
		_ = d.listener.Close()
	}
	for w := range d.workers {
		_ = w.Close()
	}
	d.mu.Unlock()
	os.Exit(0)
}

func (d *Daemon) removeWorker(w *Worker) {
	d.mu.Lock()
	delete(d.workers, w)
	d.mu.Unlock()
}

// idleWorkerPids 返回 ACP 流量静默超过 reaper.idle_minutes 的 worker PID。
//
// 只把这些 worker 交给 reaper：仍有可能在跑任务的 worker 必须整组排除。
// 单看进程 CPU 会误判——等待模型响应、等待用户批准工具调用、等待子代理
// 返回时 harness 都是 0 CPU，此时被杀会让当前 turn 永久卡死。
func (d *Daemon) idleWorkerPids() []int {
	if d.cfg == nil || d.cfg.Reaper.IdleMinutes <= 0 {
		return nil
	}
	cutoff := time.Now().Add(-time.Duration(d.cfg.Reaper.IdleMinutes) * time.Minute)

	d.mu.Lock()
	defer d.mu.Unlock()

	var pids []int
	for w := range d.workers {
		if w == nil || w.Cmd == nil || w.Cmd.Process == nil {
			continue
		}
		if w.LastActive().Before(cutoff) {
			pids = append(pids, w.Cmd.Process.Pid)
		}
	}
	return pids
}

func (d *Daemon) getWorkerPids() []int {
	d.mu.Lock()
	defer d.mu.Unlock()
	var pids []int
	for w := range d.workers {
		if w != nil && w.Cmd != nil && w.Cmd.Process != nil {
			pids = append(pids, w.Cmd.Process.Pid)
		}
	}
	return pids
}

func sendResponse(w io.Writer, resp PoolResponse) error {
	data, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = w.Write(data)
	return err
}

// touchReader / touchWriter 在转发 ACP 字节流时刷新 worker 的最近活跃时间，
// 供 reaper 判断整个 worker 是否真的静默（而不是仅看进程 CPU）。
type touchReader struct {
	r io.Reader
	w *Worker
}

func (t touchReader) Read(p []byte) (int, error) {
	n, err := t.r.Read(p)
	if n > 0 {
		t.w.Touch()
	}
	return n, err
}

type touchWriter struct {
	w     io.Writer
	track *Worker
}

func (t touchWriter) Write(p []byte) (int, error) {
	n, err := t.w.Write(p)
	if n > 0 {
		t.track.Touch()
	}
	return n, err
}
