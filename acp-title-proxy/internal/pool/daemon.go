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

	listener    net.Listener
	standbyChan chan *Worker

	mu         sync.Mutex
	closed     bool
	workers    map[*Worker]bool
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

	// Start background replenisher loop
	go d.replenishLoop(stopCh)
	d.triggerReplenish()

	// Accept loop
	go func() {
		<-stopCh
		d.mu.Lock()
		d.closed = true
		if d.listener != nil {
			_ = d.listener.Close()
		}
		// Terminate all remaining workers
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

			// Check if we already have a standby worker
			if len(d.standbyChan) > 0 {
				continue
			}

			// Pre-warm a new worker
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

	// Automatically inject proxy and Zed CA certificates into pre-warmed worker
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

	// Wait for warmup duration or until process dies / daemon stops
	timer := time.NewTimer(d.warmupDuration)
	defer timer.Stop()

	select {
	case <-stopCh:
		_ = w.Close()
		return nil, fmt.Errorf("daemon stopping")
	case waitErr := <-w.WaitErr:
		_ = w.Close()
		return nil, fmt.Errorf("worker died during warmup: %v", waitErr)
	case <-timer.C:
		// Warmup completed successfully
		return w, nil
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

	case "status":
		d.mu.Lock()
		count := len(d.standbyChan)
		d.mu.Unlock()
		_ = sendResponse(conn, PoolResponse{
			Status: "ok",
			Msg:    fmt.Sprintf("standby_count=%d", count),
		})

	case "acquire":
		// Try to acquire standby worker
		var worker *Worker
		select {
		case worker = <-d.standbyChan:
		case <-time.After(500 * time.Millisecond):
			// No standby worker available right now
		}

		if worker != nil {
			// Auto-upgrade detection: Check if Zed downloaded a newer AGY version
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

		// Success! Notify client
		if err := sendResponse(conn, PoolResponse{Status: "ok", Msg: "worker_assigned"}); err != nil {
			_ = worker.Close()
			d.removeWorker(worker)
			d.triggerReplenish()
			return
		}

		// Trigger background replenishment for next session
		d.triggerReplenish()

		// Splice streams between TCP conn and Worker process
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

	// Inbound: client (via reader) -> worker.Stdin
	go func() {
		defer wg.Done()
		defer func() {
			if worker.Stdin != nil {
				_ = worker.Stdin.Close()
			}
		}()

		// Directly copy from reader which handles pre-buffered bytes automatically
		_, err := io.Copy(worker.Stdin, reader)
		if err != nil && err != io.EOF {
			abort()
		}
	}()

	// Outbound: worker.Stdout -> client conn
	go func() {
		defer wg.Done()
		defer abort()

		_, _ = io.Copy(conn, worker.Stdout)
	}()

	// Wait until outbound finishes (process exits/EOF) or abort is called
	select {
	case <-done:
	}

	wg.Wait()
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

func (d *Daemon) removeWorker(w *Worker) {
	d.mu.Lock()
	delete(d.workers, w)
	d.mu.Unlock()
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
