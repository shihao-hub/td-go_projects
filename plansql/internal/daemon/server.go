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

// Server 是 plansql 的守护进程核心服务。
// GUI 内嵌服务与 CLI `serve` 共用同一个 Server 与同一份 Handler，
// 不允许出现「GUI 有页面、CLI 只有裸 API」的分裂。
type Server struct {
	mu           sync.RWMutex
	addr         string
	rootDir      string
	sqlFile      string
	store        *core.Store
	wal          *core.WALManager
	scanner      *core.Scanner
	noAddrFile   bool
	onRootChange func(rootDir, sqlFile string)
	httpServer   *http.Server
	idleTracker  *IdleTracker
	stopOnce     sync.Once
}

// Config 启动配置
type Config struct {
	Addr        string
	RootDir     string
	SQLFile     string
	IdleTimeout time.Duration
	AutoExit    bool
	// NoAddrFile 为 true 时不读写 plansql.addr，也不在 Stop 时清除地址文件。
	// GUI 内嵌服务必须开启：它是按项目根隔离的临时服务，不能冒充可被 CLI 发现的共享 daemon。
	NoAddrFile bool
	// OnRootChange 在项目根切换成功后回调，供桌面壳持久化「上次打开的项目」。
	OnRootChange func(rootDir, sqlFile string)
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
	cfg.RootDir = filepath.Clean(cfg.RootDir)
	if cfg.SQLFile == "" {
		cfg.SQLFile = filepath.Join(cfg.RootDir, "plans_status.sql")
	}
	if cfg.IdleTimeout == 0 {
		cfg.IdleTimeout = 30 * time.Minute
	}

	store, wal, scanner, err := buildProjectState(cfg.RootDir, cfg.SQLFile)
	if err != nil {
		return nil, err
	}

	s := &Server{
		addr:         cfg.Addr,
		rootDir:      cfg.RootDir,
		sqlFile:      cfg.SQLFile,
		store:        store,
		wal:          wal,
		scanner:      scanner,
		noAddrFile:   cfg.NoAddrFile,
		onRootChange: cfg.OnRootChange,
	}

	if cfg.AutoExit {
		s.idleTracker = NewIdleTracker(cfg.IdleTimeout, func() {
			fmt.Printf("[daemon] idle timeout (%v) reached, shutting down...\n", cfg.IdleTimeout)
			_ = s.Stop()
		})
	}

	return s, nil
}

// buildProjectState 为指定项目根重建投影库、WAL 管理器与扫描器。
func buildProjectState(rootDir, sqlFile string) (*core.Store, *core.WALManager, *core.Scanner, error) {
	store, err := core.NewMemoryStore()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("init memory store: %w", err)
	}

	wal := core.NewWALManager(sqlFile)
	// 启动时自动从 SQL 文件重放现有历史数据
	if err := wal.Replay(store); err != nil {
		_ = store.Close()
		return nil, nil, nil, fmt.Errorf("replay existing sql: %w", err)
	}

	return store, wal, core.NewScanner(rootDir), nil
}

// Handler 返回 GUI 与 CLI serve 共用的完整 HTTP Handler（REST + 内嵌 WebUI）。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/v1/meta", s.wrapHandler(s.handleMeta))
	mux.HandleFunc("/api/v1/root", s.wrapHandler(s.handleRoot))
	mux.HandleFunc("/api/v1/items", s.wrapHandler(s.handleGetItems))
	mux.HandleFunc("/api/v1/check", s.wrapHandler(s.handleCheck))
	mux.HandleFunc("/api/v1/scan", s.wrapHandler(s.handleScan))
	mux.HandleFunc("/api/v1/append", s.wrapHandler(s.handleAppend))

	// 嵌入静态 Web 看板
	mux.Handle("/", web.Handler())

	return mux
}

// Listen 在指定地址上绑定监听器；addr 中的端口为 0 时由内核动态探测。
func Listen(addr string) (net.Listener, error) {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w", addr, err)
	}
	return listener, nil
}

// URL 由监听器推导出实际可访问地址，供窗口、托盘与 CLI 输出共用。
func URL(listener net.Listener) string {
	return "http://" + listener.Addr().String()
}

// Serve 在调用方已绑定的监听器上提供服务。
func (s *Server) Serve(listener net.Listener) error {
	srv := &http.Server{Handler: s.Handler()}
	s.mu.Lock()
	s.httpServer = srv
	s.mu.Unlock()
	return srv.Serve(listener)
}

// Start 启动 HTTP 守护进程并写入地址文件，阻塞直至服务退出。
func (s *Server) Start() error {
	listener, err := Listen(s.addr)
	if err != nil {
		return err
	}

	if !s.noAddrFile {
		if err := WriteAddressFile(listener.Addr().String()); err != nil {
			_ = listener.Close()
			return fmt.Errorf("write address file: %w", err)
		}
	}

	fmt.Printf("[daemon] plansql serve started on %s\n", URL(listener))
	rootDir, sqlFile := s.Project()
	fmt.Printf("[daemon] watching root: %s, sql: %s\n", rootDir, sqlFile)

	return s.Serve(listener)
}

// Stop 停止守护进程并清理地址文件
func (s *Server) Stop() error {
	var err error
	s.stopOnce.Do(func() {
		if !s.noAddrFile {
			_ = RemoveAddressFile()
		}

		s.mu.Lock()
		srv := s.httpServer
		s.httpServer = nil
		store := s.store
		s.store = nil
		s.mu.Unlock()

		if srv != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			err = srv.Shutdown(ctx)
		}
		if store != nil {
			_ = store.Close()
		}
	})
	return err
}

// Project 返回当前项目根与 SQL 文件路径。
func (s *Server) Project() (rootDir, sqlFile string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.rootDir, s.sqlFile
}

// Reconfigure 原子切换项目根：先在后台重建投影并重放 SQL，成功后才替换旧状态。
// 失败时旧状态保持不变，调用方可安全提示用户重试。
func (s *Server) Reconfigure(rootDir, sqlFile string) error {
	if rootDir == "" {
		return fmt.Errorf("项目根目录不能为空")
	}
	rootDir = filepath.Clean(rootDir)
	if sqlFile == "" {
		sqlFile = filepath.Join(rootDir, "plans_status.sql")
	}
	info, err := os.Stat(rootDir)
	if err != nil {
		return fmt.Errorf("项目根目录不可访问: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("项目根路径不是目录: %s", rootDir)
	}

	store, wal, scanner, err := buildProjectState(rootDir, sqlFile)
	if err != nil {
		return err
	}

	s.mu.Lock()
	oldStore := s.store
	s.store, s.wal, s.scanner = store, wal, scanner
	s.rootDir, s.sqlFile = rootDir, sqlFile
	onChange := s.onRootChange
	s.mu.Unlock()

	if oldStore != nil {
		_ = oldStore.Close()
	}
	if onChange != nil {
		onChange(rootDir, sqlFile)
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
