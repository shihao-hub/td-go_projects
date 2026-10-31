package main

import (
	"bufio"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"language_projects/go_projects/acp-title-proxy/internal/agy"
	"language_projects/go_projects/acp-title-proxy/internal/applog"
	"language_projects/go_projects/acp-title-proxy/internal/config"
	"language_projects/go_projects/acp-title-proxy/internal/finder"
	"language_projects/go_projects/acp-title-proxy/internal/mux"
	"language_projects/go_projects/acp-title-proxy/internal/opencode"
	"language_projects/go_projects/acp-title-proxy/internal/pool"
	"language_projects/go_projects/acp-title-proxy/internal/proxy"
	"language_projects/go_projects/acp-title-proxy/internal/skill"
	"language_projects/go_projects/acp-title-proxy/internal/syncer"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmd := strings.ToLower(os.Args[1])

	// 日志同时落盘到 %APPDATA%\language_projects\acp-title-proxy\acp-title-proxy.log，
	// 因为 Zed 不会转发 agent 子进程的 stderr，仅靠终端无法排查。
	closeLog := applog.Init()
	defer closeLog()

	cfg, cfgPath, err := config.LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[acp-title-proxy] Warning loading config: %v\n", err)
	}
	skillMgr := skill.NewManager(&cfg.Skills)

	switch cmd {
	case "share":
		// 共享后端：1 个 agy_acp_server 承载多个 ACP 会话
		d := mux.NewDaemon(cfg, log.Default(), nil)
		d.SetTitler(agy.NewTitler(&cfg.AGY, d, nil))
		d.SetSkillManager(skillMgr)
		if err := d.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "[acp-title-proxy] share daemon error: %v\n", err)
			os.Exit(1)
		}
		d.Shutdown()

	case "serve":
		// Zed 每个 agent panel 启动一个 serve，连接共享 daemon
		if err := mux.RunServe(cfg, log.Default()); err != nil {
			fmt.Fprintf(os.Stderr, "[acp-title-proxy] serve error: %v\n", err)
			os.Exit(1)
		}

	case "share-status":
		os.Exit(shareStatus(cfg))

	case "share-stop":
		os.Exit(shareStop(cfg))

	case "share-restart":
		_ = shareStop(cfg)
		time.Sleep(300 * time.Millisecond)
		if err := mux.LaunchDaemon(); err != nil {
			fmt.Fprintf(os.Stderr, "[acp-title-proxy] failed to start share daemon: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("[acp-title-proxy] share daemon restarted in background.")

	case "sync":
		// 模式 A：一次性同步（完全外部化，不改动 settings.json）
		fmt.Printf("[acp-title-proxy] Running one-shot sync (Config: %s)...\n", cfgPath)
		s := syncer.NewSyncer(cfg)

		ocCount, err := s.SyncOpenCode()
		if err != nil {
			fmt.Fprintf(os.Stderr, "[OpenCode] Sync error: %v\n", err)
		} else {
			fmt.Printf("[OpenCode] Successfully synced %d titles from opencode.db\n", ocCount)
		}

		// 同步最近 24 小时内更新的 AGY 线程
		agyCount, err := s.SyncAGY(24 * time.Hour)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[AGY] Sync error: %v\n", err)
		} else {
			fmt.Printf("[AGY] Successfully synced %d titles via LLM (OpenCode Go / DeepSeek)\n", agyCount)
		}

	case "watch":
		// 模式 A：后台守护进程监听（完全外部化，不改动 settings.json）
		fmt.Printf("[acp-title-proxy] Starting background watcher (Config: %s)...\n", cfgPath)
		fmt.Println("[acp-title-proxy] Watching Zed db.sqlite for new threads. Press Ctrl+C to stop.")
		s := syncer.NewSyncer(cfg)

		stopCh := make(chan struct{})
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

		go func() {
			<-sigCh
			fmt.Println("\n[acp-title-proxy] Shutting down watcher...")
			close(stopCh)
		}()

		// 每 3 秒轮询一次；AGY 仅同步最近 2 小时内更新的线程
		s.Watch(3*time.Second, 2*time.Hour, stopCh)

	case "daemon":
		// 为 AGY 预热连接池守护进程
		fmt.Printf("[acp-title-proxy] Starting AGY pre-warm pool daemon (Port: %d)...\n", pool.DefaultPort)
		stopCh := make(chan struct{})
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

		go func() {
			<-sigCh
			fmt.Println("\n[acp-title-proxy] Shutting down pre-warm daemon...")
			close(stopCh)
		}()

		d := pool.NewDaemon(cfg, pool.DefaultPort, 8*time.Second)
		if err := d.Start(stopCh); err != nil {
			fmt.Fprintf(os.Stderr, "[acp-title-proxy] Daemon error: %v\n", err)
			os.Exit(1)
		}

	case "status":
		resp, err := pool.CheckStatus(pool.DefaultPort)
		if err != nil {
			fmt.Printf("[acp-title-proxy] Pre-warm daemon is NOT running (%v)\n", err)
		} else {
			fmt.Printf("[acp-title-proxy] Pre-warm daemon is running: %s (Status: %s)\n", resp.Msg, resp.Status)
		}

	case "stop":
		err := pool.StopDaemon(pool.DefaultPort)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[acp-title-proxy] %v\n", err)
			os.Exit(1)
		}
		fmt.Println("[acp-title-proxy] Pre-warm daemon stopped successfully.")

	case "restart":
		_ = pool.StopDaemon(pool.DefaultPort)
		time.Sleep(200 * time.Millisecond)
		if err := pool.LaunchDaemonInBackground(); err != nil {
			fmt.Fprintf(os.Stderr, "[acp-title-proxy] Failed to start daemon: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("[acp-title-proxy] Pre-warm daemon restarted successfully in background.")

	case "opencode":
		// 模式 B：面向 OpenCode 的透明 stdio 代理
		inj := proxy.NewInjector(os.Stdout)
		exePath, args, err := finder.FindOpenCode(cfg.OpenCode.Executable)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[acp-title-proxy] Error: %v\n", err)
			os.Exit(1)
		}
		if len(os.Args) > 2 {
			args = append(args, os.Args[2:]...)
		}
		titler := opencode.NewTitler(&cfg.OpenCode, inj)
		runner := proxy.NewRunner(exePath, args, titler, skillMgr)
		if err := runner.Run(inj); err != nil {
			os.Exit(1)
		}

	case "agy", "antigravity", "antigravity-acp":
		// agy share：共享后端模式（多 panel 复用一个 worker）
		if len(os.Args) > 2 && strings.EqualFold(os.Args[2], "share") {
			if err := mux.RunServe(cfg, log.Default()); err != nil {
				fmt.Fprintf(os.Stderr, "[acp-title-proxy] serve error: %v\n", err)
				os.Exit(1)
			}
			return
		}

		// 模式 B：带预热池加速的 AGY stdio 代理
		inj := proxy.NewInjector(os.Stdout)
		titler := agy.NewTitler(&cfg.AGY, inj, nil)

		// 1. 优先从守护进程获取已预热的 worker（即时响应）
		conn, err := pool.TryAcquire(pool.DefaultPort, 400*time.Millisecond)
		if err == nil {
			runner := proxy.NewRunner("", nil, titler, skillMgr)
			if err := runner.RunStream(conn, inj); err != nil {
				os.Exit(1)
			}
			return
		}

		if errors.Is(err, pool.ErrDaemonOutdated) {
			fmt.Fprintf(os.Stderr, "[acp-title-proxy] Outdated daemon detected and retiring; launching updated daemon in background...\n")
			time.Sleep(150 * time.Millisecond)
			_ = pool.LaunchDaemonInBackground()
		} else {
			// 2. 兜底：守护进程未就绪，静默拉起守护进程供后续会话使用
			fmt.Fprintf(os.Stderr, "[acp-title-proxy] Pre-warm pool not available (%v); starting daemon in background and falling back to direct launch...\n", err)
			_ = pool.LaunchDaemonInBackground()
		}

		// 3. 兜底：直接启动
		exePath, args, err := finder.FindAGY(cfg.AGY.Executable)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[acp-title-proxy] Error: %v\n", err)
			os.Exit(1)
		}
		if len(os.Args) > 2 {
			args = append(args, os.Args[2:]...)
		}
		runner := proxy.NewRunner(exePath, args, titler, skillMgr)
		if err := runner.Run(inj); err != nil {
			os.Exit(1)
		}

	default:
		fmt.Fprintf(os.Stderr, "[acp-title-proxy] Unknown command: %s\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

func shareAddr(cfg *config.Config) string {
	return fmt.Sprintf("127.0.0.1:%d", cfg.Share.Port)
}

func shareStatus(cfg *config.Config) int {
	conn, err := net.DialTimeout("tcp", shareAddr(cfg), time.Second)
	if err != nil {
		fmt.Println("[acp-title-proxy] share daemon is NOT running:", err)
		return 1
	}
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	_, _ = conn.Write([]byte(`{"cmd":"status"}` + "\n"))
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		fmt.Println("[acp-title-proxy] failed to read share status:", err)
		return 1
	}
	fmt.Println(strings.TrimSpace(string(line)))
	return 0
}

func shareStop(cfg *config.Config) int {
	conn, err := net.DialTimeout("tcp", shareAddr(cfg), time.Second)
	if err != nil {
		fmt.Println("[acp-title-proxy] share daemon is NOT running")
		return 0
	}
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	_, _ = conn.Write([]byte(`{"cmd":"stop"}` + "\n"))
	_, _ = bufio.NewReader(conn).ReadBytes('\n')
	fmt.Println("[acp-title-proxy] share daemon stopped")
	return 0
}

func printUsage() {
	fmt.Fprintf(os.Stderr, "acp-title-proxy - Zed ACP Title Enhancement Utility\n\n")
	fmt.Fprintf(os.Stderr, "Mode A (Zero-intrusion, settings.json untouched, 100%% upgrade-safe):\n")
	fmt.Fprintf(os.Stderr, "  acp-title-proxy sync     - One-shot sync: syncs all OpenCode & recent AGY titles\n")
	fmt.Fprintf(os.Stderr, "  acp-title-proxy watch    - Background watcher: auto-syncs newly created threads\n\n")
	fmt.Fprintf(os.Stderr, "Mode B (Stdio proxy wrapper with Pre-warm Pool):\n")
	fmt.Fprintf(os.Stderr, "  acp-title-proxy opencode - Proxy runner for OpenCode\n")
	fmt.Fprintf(os.Stderr, "  acp-title-proxy agy      - Proxy runner for AGY (uses pre-warm pool if available)\n")
	fmt.Fprintf(os.Stderr, "  acp-title-proxy daemon   - Pre-warm pool background service for AGY\n")
	fmt.Fprintf(os.Stderr, "  acp-title-proxy status   - Query pre-warm pool status\n")
	fmt.Fprintf(os.Stderr, "  acp-title-proxy stop     - Gracefully stop pre-warm pool daemon\n")
	fmt.Fprintf(os.Stderr, "  acp-title-proxy restart  - Restart pre-warm pool daemon in background\n\n")
	fmt.Fprintf(os.Stderr, "Shared backend (1 AGY worker, N concurrent ACP sessions):\n")
	fmt.Fprintf(os.Stderr, "  acp-title-proxy share         - Start the shared backend daemon\n")
	fmt.Fprintf(os.Stderr, "  acp-title-proxy serve         - Per-panel client for Zed (attach to shared daemon)\n")
	fmt.Fprintf(os.Stderr, "  acp-title-proxy share-status  - Query shared backend status\n")
	fmt.Fprintf(os.Stderr, "  acp-title-proxy share-stop    - Stop shared backend and its worker\n")
	fmt.Fprintf(os.Stderr, "  acp-title-proxy share-restart - Restart shared backend in background\n")
}
