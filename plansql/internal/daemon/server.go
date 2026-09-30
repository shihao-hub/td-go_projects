package daemon

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"plansql/internal/core"
	"plansql/internal/web"
	"sync"
	"time"
)

// Server 是 plansql 的守护进程核心服务
type Server struct {
	addr        string
	rootDir     string
	sqlFile     string
	store       *core.Store
	wal         *core.WALManager
	scanner     *core.Scanner
	httpServer  *http.Server
	idleTracker *IdleTracker
	mu          sync.Mutex
}

// Config 启动配置
type Config struct {
	Addr        string
	RootDir     string
	SQLFile     string
	IdleTimeout time.Duration
	AutoExit    bool
}

// NewServer 创建守护进程服务器
func NewServer(cfg Config) (*Server, error) {
	if cfg.Addr == "" {
		cfg.Addr = "127.0.0.1:18090"
	}
	if cfg.RootDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("get cwd: %w", err)
		}
		cfg.RootDir = cwd
	}
	if cfg.SQLFile == "" {
		cfg.SQLFile = filepath.Join(cfg.RootDir, "plans_status.sql")
	}
	if cfg.IdleTimeout == 0 {
		cfg.IdleTimeout = 30 * time.Minute
	}

	store, err := core.NewMemoryStore()
	if err != nil {
		return nil, fmt.Errorf("init memory store: %w", err)
	}

	wal := core.NewWALManager(cfg.SQLFile)
	// 启动时自动从 SQL 文件重放现有历史数据
	if err := wal.Replay(store); err != nil {
		store.Close()
		return nil, fmt.Errorf("replay existing sql: %w", err)
	}

	scanner := core.NewScanner(cfg.RootDir)

	s := &Server{
		addr:    cfg.Addr,
		rootDir: cfg.RootDir,
		sqlFile: cfg.SQLFile,
		store:   store,
		wal:     wal,
		scanner: scanner,
	}

	if cfg.AutoExit {
		s.idleTracker = NewIdleTracker(cfg.IdleTimeout, func() {
			fmt.Printf("[daemon] idle timeout (%v) reached, shutting down...\n", cfg.IdleTimeout)
			_ = s.Stop()
		})
	}

	return s, nil
}

// Start 启动 HTTP 守护进程
func (s *Server) Start() error {
	mux := http.NewServeMux()

	// API 路由
	mux.HandleFunc("/api/v1/items", s.wrapHandler(s.handleGetItems))
	mux.HandleFunc("/api/v1/check", s.wrapHandler(s.handleCheck))
	mux.HandleFunc("/api/v1/scan", s.wrapHandler(s.handleScan))
	mux.HandleFunc("/api/v1/append", s.wrapHandler(s.handleAppend))

	// 嵌入静态 Web 看板
	mux.Handle("/", web.Handler())

	listener, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", s.addr, err)
	}

	s.httpServer = &http.Server{
		Handler: mux,
	}

	// 写入地址文件供客户端自动发现
	if err := WriteAddressFile(listener.Addr().String()); err != nil {
		listener.Close()
		return fmt.Errorf("write address file: %w", err)
	}

	fmt.Printf("[daemon] plansql serve started on http://%s\n", listener.Addr().String())
	fmt.Printf("[daemon] watching root: %s, sql: %s\n", s.rootDir, s.sqlFile)

	return s.httpServer.Serve(listener)
}

// Stop 停止守护进程并清理地址文件
func (s *Server) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_ = RemoveAddressFile()
	if s.store != nil {
		_ = s.store.Close()
	}
	if s.httpServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		return s.httpServer.Shutdown(ctx)
	}
	return nil
}

// wrapHandler 包装请求用于闲置超时追踪
func (s *Server) wrapHandler(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.idleTracker != nil {
			s.idleTracker.BeginRequest()
			defer s.idleTracker.EndRequest()
		}
		h(w, r)
	}
}

// SpawnDaemonDetached 在后台以 DETACHED 模式拉起自身作为守护进程
func SpawnDaemonDetached(exePath, rootDir, sqlFile string) error {
	args := []string{"serve", "--root", rootDir, "--sql", sqlFile, "--auto-exit"}
	cmd := exec.Command(exePath, args...)
	cmd.Dir = rootDir
	// Windows 独立进程组分离
	setSysProcAttr(cmd)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start detached daemon: %w", err)
	}
	return nil
}
