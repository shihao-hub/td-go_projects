// Package daemon 是 agyquota 的唯一业务进程：装配 service、在回环地址
// 暴露 HTTP+JSON API、维护地址文件与 buildID 握手，并支持空闲自动退出。
package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"agyquota/internal/appdata"
	"agyquota/internal/buildinfo"
	"agyquota/internal/service"
)

// Config 是 daemon 的启动配置。
type Config struct {
	Bind        string        // 监听地址，默认 127.0.0.1
	Port        int           // 监听端口，0 表示随机可用端口
	IdleTimeout time.Duration // 空闲自动退出时长，0 表示不退出（前台 serve）
	DataDir     string        // 数据目录，空取 appdata.Dir()（测试注入临时目录）
}

// Serve 启动 daemon 并阻塞至退出（前台/自动拉起共用入口）。
func Serve(ctx context.Context, cfg Config) error {
	return serve(ctx, cfg, service.New())
}

// serve 是 Serve 的测试接缝：允许注入带 Fetcher 的 Service。
func serve(ctx context.Context, cfg Config, svc *service.Service) error {
	bind := cfg.Bind
	if bind == "" {
		bind = "127.0.0.1"
	}
	dataDir := cfg.DataDir
	if dataDir == "" {
		dataDir = appdata.Dir()
	}
	if dataDir == "" {
		return errors.New("无法解析数据目录（APPDATA/HOME 均不可用）")
	}
	addrPath := filepath.Join(dataDir, appdata.AddressFileName)

	ln, err := net.Listen("tcp", net.JoinHostPort(bind, strconv.Itoa(cfg.Port)))
	if err != nil {
		return fmt.Errorf("监听 %s:%d 失败: %w", bind, cfg.Port, err)
	}
	addr := ln.Addr().String()

	rootCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	logger := log.New(os.Stderr, "[agyquota-daemon] ", log.LstdFlags)
	started := time.Now()

	h := &handler{
		svc:     svc,
		version: buildinfo.Version,
		buildID: buildinfo.BuildID,
		pid:     os.Getpid(),
		started: started,
	}
	h.lastActivity.Store(started.UnixNano())

	srv := &http.Server{
		Handler:           h.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		// BaseContext 挂在根 ctx 上：关闭时 cancel 根 ctx 能中断在途 handler。
		BaseContext: func(net.Listener) context.Context { return rootCtx },
	}

	// 地址文件只在监听成功后原子写入，退出（含空闲退出）即删除。
	rec := addressRecord{
		Schema:    1,
		Addr:      addr,
		Version:   buildinfo.Version,
		BuildID:   buildinfo.BuildID,
		PID:       os.Getpid(),
		StartedAt: started,
	}
	if err := writeAddressFile(addrPath, rec); err != nil {
		_ = ln.Close()
		return fmt.Errorf("写入地址文件失败: %w", err)
	}

	var shutdownOnce sync.Once
	shutdownDone := make(chan struct{})
	triggerShutdown := func(reason string) {
		shutdownOnce.Do(func() {
			defer close(shutdownDone)
			h.shuttingDown.Store(true)
			logger.Printf("开始退出（%s）", reason)
			cancel()
			shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer shutCancel()
			_ = srv.Shutdown(shutCtx)
			if err := os.Remove(addrPath); err != nil && !os.IsNotExist(err) {
				logger.Printf("删除地址文件失败: %v", err)
			}
			logger.Printf("daemon 已退出")
		})
	}
	h.triggerShutdown = triggerShutdown

	logger.Printf("daemon 已启动: http://%s (version=%s buildID=%s pid=%d idle=%s)",
		addr, buildinfo.Version, buildinfo.BuildID, os.Getpid(), cfg.IdleTimeout)

	// 信号与外部 ctx 取消走同一优雅退出路径。
	sigCtx, stopSignals := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	go func() {
		select {
		case <-sigCtx.Done():
			if ctx.Err() == nil {
				triggerShutdown("收到退出信号")
			}
		case <-ctx.Done():
			triggerShutdown("上下文取消")
		case <-rootCtx.Done():
		}
	}()

	// 空闲监控：inflight==0 且空闲超过 IdleTimeout 才判定；
	// 在途请求绝不因空闲被杀（请求级超时由客户端 ctx 控制）。
	if cfg.IdleTimeout > 0 {
		period := idleCheckPeriod(cfg.IdleTimeout)
		go func() {
			t := time.NewTicker(period)
			defer t.Stop()
			for {
				select {
				case <-rootCtx.Done():
					return
				case <-t.C:
					if h.inflight.Load() > 0 {
						continue
					}
					last := time.Unix(0, h.lastActivity.Load())
					if time.Since(last) > cfg.IdleTimeout {
						triggerShutdown(fmt.Sprintf("空闲超过 %s", cfg.IdleTimeout))
						return
					}
				}
			}
		}()
	}

	serveErr := srv.Serve(ln)
	if errors.Is(serveErr, http.ErrServerClosed) {
		// Shutdown 关闭监听后 Serve 立即返回，但收尾（删地址文件/退出日志）
		// 仍在 triggerShutdown goroutine 内进行；必须等它完成再返回，
		// 否则进程提前退出会跳过收尾。
		if h.shuttingDown.Load() {
			<-shutdownDone
		}
		return nil
	}
	if serveErr != nil {
		_ = os.Remove(addrPath)
		return serveErr
	}
	return nil
}

// idleCheckPeriod 空闲检查周期：clamp(IdleTimeout/2, 50ms, 30s)。
func idleCheckPeriod(idle time.Duration) time.Duration {
	p := idle / 2
	if p < 50*time.Millisecond {
		p = 50 * time.Millisecond
	}
	if p > 30*time.Second {
		p = 30 * time.Second
	}
	return p
}

// addressRecord 是地址文件的落盘结构（client 只读其中 addr）。
type addressRecord struct {
	Schema    int       `json:"schema"`
	Addr      string    `json:"addr"`
	Version   string    `json:"version"`
	BuildID   string    `json:"buildID"`
	PID       int       `json:"pid"`
	StartedAt time.Time `json:"startedAt"`
}

// writeAddressFile 临时文件 + rename 原子写入地址文件。
func writeAddressFile(path string, rec addressRecord) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
