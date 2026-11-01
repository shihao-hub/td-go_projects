package main

import (
	"bufio"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"language_projects/go_projects/acp-title-proxy/internal/agy"
	"language_projects/go_projects/acp-title-proxy/internal/applog"
	"language_projects/go_projects/acp-title-proxy/internal/config"
	"language_projects/go_projects/acp-title-proxy/internal/finder"
	"language_projects/go_projects/acp-title-proxy/internal/governor"
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

	// 日志同时落盘到 %APPDATA%\language_projects\acp-title-proxy\ 下的角色文件，
	// 因为 Zed 不会转发 agent 子进程的 stderr，仅靠终端无法排查。
	// 长驻 daemon 与多进程客户端分开落盘：前者独占文件走标准轮转，
	// 后者可能被多个 panel 进程并发写，需要另一套轮转策略。
	role := applog.RoleClient
	switch cmd {
	case "share":
		role = applog.RoleShare
	case "daemon":
		role = applog.RolePool
	}
	closeLog := applog.Init(role)
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

	case "update", "update-agy":
		// 顶层别名：检查并下载 AGY 官方最新版本
		os.Exit(runUpdate(os.Args[2:]))

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

		// agy threads：管理 Zed 侧会话的 agent 归属与归档状态
		if len(os.Args) > 2 && strings.EqualFold(os.Args[2], "threads") {
			if code := runThreads(os.Args[3:]); code != 0 {
				os.Exit(code)
			}
			return
		}

		// agy sessions：列出直连模式下当前受管的活跃会话
		if len(os.Args) > 2 && (strings.EqualFold(os.Args[2], "sessions") || strings.EqualFold(os.Args[2], "list-sessions")) {
			gov := governor.NewGovernor(3)
			list := gov.List()
			if len(list) == 0 {
				fmt.Println("[acp-title-proxy] 当前无受管活跃 AGY 会话")
				return
			}
			fmt.Printf("[acp-title-proxy] 受管活跃 AGY 会话清单 (%d 个):\n", len(list))
			for _, s := range list {
				fmt.Printf("  PID=%-7d 状态=%-5s 创建=%s 最近活跃=%s 会话ID=%s\n",
					s.PID, s.State, s.CreatedAt.Format("15:04:05"), s.LastActiveAt.Format("15:04:05"), s.SessionID)
			}
			return
		}

		// agy reap：清理父进程已退出的 AGY 孤儿进程（内存泄漏兜底）
		if len(os.Args) > 2 && strings.EqualFold(os.Args[2], "reap") {
			dryRun := false
			for _, a := range os.Args[3:] {
				if a == "--dry-run" || a == "-n" {
					dryRun = true
				}
			}
			res := pool.ReapOrphans(dryRun)
			if len(res.Orphans) == 0 {
				fmt.Println("[agy reap] 未发现孤儿 AGY 进程")
				return
			}
			for _, p := range res.Orphans {
				fmt.Printf("  %s pid=%d parent=%d %dMB created=%s\n", p.Name, p.PID, p.Parent, p.MemoryMB, p.Created)
			}
			if dryRun {
				fmt.Printf("[agy reap] dry-run：可清理 %d 个，约 %d MB\n", len(res.Orphans), res.FreedMB)
				return
			}
			fmt.Printf("[agy reap] 已清理 %d 个，释放约 %d MB\n", res.Reaped, res.FreedMB)
			for _, f := range res.Failures {
				fmt.Fprintf(os.Stderr, "[agy reap] 失败: %s\n", f)
			}
			return
		}

		// agy reap-idle：只读诊断——按 worker 分组列出全部 harness 及其活跃状态
		if len(os.Args) > 2 && strings.EqualFold(os.Args[2], "reap-idle") {
			r := pool.NewReaper(cfg.Reaper, nil)
			groups := r.Collect()
			total := 0
			for owner, list := range groups {
				total += len(list)
				fmt.Printf("worker %d: %d harness\n", owner, len(list))
				for _, h := range list {
					fmt.Printf("   pid=%d parent=%d created=%s\n", h.PID, h.ParentPID, h.CreatedAt.Format("15:04:05"))
				}
			}
			fmt.Printf("[reap-idle] %d harness；daemon 按每个 harness（含工具子进程树）的 CPU 活跃度回收，连续 %dm 无增长才动手\n",
				total, cfg.Reaper.IdleMinutes)
			return
		}

		// agy update / upgrade：检查并下载更新 AGY 官方二进制
		if len(os.Args) > 2 && (strings.EqualFold(os.Args[2], "update") || strings.EqualFold(os.Args[2], "upgrade")) {
			if code := runUpdate(os.Args[3:]); code != 0 {
				os.Exit(code)
			}
			return
		}

		// 解析直连模式参数与会话上限
		isDirect := false
		maxSessions := 3
		var passthroughArgs []string

		for i := 2; i < len(os.Args); i++ {
			arg := os.Args[i]
			if arg == "--direct" || arg == "-direct" {
				isDirect = true
				continue
			}
			if strings.HasPrefix(arg, "--max-sessions=") {
				isDirect = true
				if n, err := strconv.Atoi(strings.TrimPrefix(arg, "--max-sessions=")); err == nil && n > 0 {
					maxSessions = n
				}
				continue
			}
			if arg == "--max-sessions" && i+1 < len(os.Args) {
				isDirect = true
				i++
				if n, err := strconv.Atoi(os.Args[i]); err == nil && n > 0 {
					maxSessions = n
				}
				continue
			}
			passthroughArgs = append(passthroughArgs, arg)
		}

		// 直连模式（轻量透明壳子，无预热池守护进程，由 Governor 管理并发配额）
		if isDirect {
			gov := governor.NewGovernor(maxSessions)
			if err := gov.Acquire(); err != nil {
				fmt.Fprintf(os.Stderr, "[acp-title-proxy] %v\n", err)
				os.Exit(1)
			}
			defer gov.Release()

			inj := proxy.NewInjector(os.Stdout)
			titler := agy.NewTitler(&cfg.AGY, inj, nil)

			exePath, args, err := finder.FindAGY(cfg.AGY.Executable)
			if err != nil {
				fmt.Fprintf(os.Stderr, "[acp-title-proxy] Error: %v\n", err)
				gov.Release()
				os.Exit(1)
			}
			if len(passthroughArgs) > 0 {
				args = append(args, passthroughArgs...)
			}

			runner := proxy.NewRunner(exePath, args, titler, skillMgr)
			runner.Tracker = gov
			runner.MaxHarnesses = maxSessions
			if err := runner.Run(inj); err != nil {
				gov.Release()
				os.Exit(1)
			}
			gov.Release()
			return
		}

		// 默认模式：带预热池加速的 AGY stdio 代理
		inj := proxy.NewInjector(os.Stdout)
		titler := agy.NewTitler(&cfg.AGY, inj, nil)

		// 1. 优先从守护进程获取已预热的 worker（即时响应）
		conn, err := pool.TryAcquire(pool.DefaultPort, 400*time.Millisecond)
		if err == nil {
			runner := proxy.NewRunner("", nil, titler, skillMgr)
			runner.MaxHarnesses = maxSessions
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
		runner.MaxHarnesses = maxSessions
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
	reader := bufio.NewReader(conn)
	line, err := reader.ReadString('\n')
	if err != nil {
		fmt.Println("[acp-title-proxy] share daemon status read error:", err)
		return 1
	}
	fmt.Print(line)
	return 0
}

func shareStop(cfg *config.Config) int {
	conn, err := net.DialTimeout("tcp", shareAddr(cfg), time.Second)
	if err != nil {
		fmt.Println("[acp-title-proxy] share daemon is NOT running (nothing to stop)")
		return 0
	}
	defer conn.Close()

	_, _ = fmt.Fprintln(conn, "STOP")
	fmt.Println("[acp-title-proxy] stop signal sent to share daemon.")
	return 0
}

func printUsage() {
	fmt.Fprintf(os.Stderr, "Usage: acp-title-proxy <command> [args...]\n\n")
	fmt.Fprintf(os.Stderr, "Mode A (Zero Zed modifications - fully external):\n")
	fmt.Fprintf(os.Stderr, "  acp-title-proxy sync     - One-shot sync: writes LLM titles to Zed db.sqlite\n")
	fmt.Fprintf(os.Stderr, "  acp-title-proxy watch    - Background watcher: auto-syncs newly created threads\n\n")
	fmt.Fprintf(os.Stderr, "Mode B (Direct Shell with Session Quota Governor - Recommended):\n")
	fmt.Fprintf(os.Stderr, "  acp-title-proxy agy --direct [--max-sessions=3] - Direct 1:1 wrapper without pre-warm pool\n")
	fmt.Fprintf(os.Stderr, "  acp-title-proxy agy sessions                    - List currently managed active sessions\n\n")
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
	fmt.Fprintf(os.Stderr, "  acp-title-proxy share-restart - Restart shared backend in background\n\n")
	fmt.Fprintf(os.Stderr, "AGY binary auto-update:\n")
	fmt.Fprintf(os.Stderr, "  acp-title-proxy agy update          - Check & download latest AGY binary to Zed registry\n")
	fmt.Fprintf(os.Stderr, "  acp-title-proxy agy update --check  - Only check for newer version\n\n")
	fmt.Fprintf(os.Stderr, "AGY orphan process reclaim (memory leak fallback):\n")
	fmt.Fprintf(os.Stderr, "  acp-title-proxy agy reap            - Kill AGY processes whose parent has exited\n")
	fmt.Fprintf(os.Stderr, "  acp-title-proxy agy reap --dry-run  - Only scan and list orphans\n\n")
	fmt.Fprintf(os.Stderr, "Zed thread management (agent ownership / archive):\n")
	fmt.Fprintf(os.Stderr, "  acp-title-proxy agy threads list    - Show sessions grouped by agent_id\n")
	fmt.Fprintf(os.Stderr, "  acp-title-proxy agy threads adopt   - Move sessions from one agent to another (dry-run)\n")
	fmt.Fprintf(os.Stderr, "  acp-title-proxy agy threads archive - Archive sessions of an agent (dry-run)\n")
}
