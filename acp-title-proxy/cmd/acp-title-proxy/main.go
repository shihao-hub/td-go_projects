package main

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"language_projects/go_projects/acp-title-proxy/internal/agy"
	"language_projects/go_projects/acp-title-proxy/internal/config"
	"language_projects/go_projects/acp-title-proxy/internal/finder"
	"language_projects/go_projects/acp-title-proxy/internal/opencode"
	"language_projects/go_projects/acp-title-proxy/internal/pool"
	"language_projects/go_projects/acp-title-proxy/internal/proxy"
	"language_projects/go_projects/acp-title-proxy/internal/syncer"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmd := strings.ToLower(os.Args[1])
	cfg, cfgPath, err := config.LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[acp-title-proxy] Warning loading config: %v\n", err)
	}

	switch cmd {
	case "sync":
		// Mode A: One-shot sync (100% external, settings.json untouched)
		fmt.Printf("[acp-title-proxy] Running one-shot sync (Config: %s)...\n", cfgPath)
		s := syncer.NewSyncer(cfg)

		ocCount, err := s.SyncOpenCode()
		if err != nil {
			fmt.Fprintf(os.Stderr, "[OpenCode] Sync error: %v\n", err)
		} else {
			fmt.Printf("[OpenCode] Successfully synced %d titles from opencode.db\n", ocCount)
		}

		// Sync AGY threads updated in last 24h
		agyCount, err := s.SyncAGY(24 * time.Hour)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[AGY] Sync error: %v\n", err)
		} else {
			fmt.Printf("[AGY] Successfully synced %d titles via LLM (OpenCode Go / DeepSeek)\n", agyCount)
		}

	case "watch":
		// Mode A: Background daemon watcher (100% external, settings.json untouched)
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

		// Poll every 3 seconds; AGY only syncs threads updated within last 2 hours
		s.Watch(3*time.Second, 2*time.Hour, stopCh)

	case "daemon":
		// Pre-warm pool daemon for AGY
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

	case "opencode":
		// Mode B: Transparent stdio proxy for OpenCode
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
		runner := proxy.NewRunner(exePath, args, titler)
		if err := runner.Run(inj); err != nil {
			os.Exit(1)
		}

	case "agy", "antigravity", "antigravity-acp":
		// Mode B: Stdio proxy for AGY with Pre-warm Pool acceleration
		inj := proxy.NewInjector(os.Stdout)
		titler := agy.NewTitler(&cfg.AGY, inj)

		// 1. Try to acquire a pre-warmed worker from daemon (Instant response!)
		conn, err := pool.TryAcquire(pool.DefaultPort, 400*time.Millisecond)
		if err == nil {
			runner := proxy.NewRunner("", nil, titler)
			if err := runner.RunStream(conn, inj); err != nil {
				os.Exit(1)
			}
			return
		}

		// 2. Fallback: Daemon not ready. Silently trigger daemon launch for future sessions.
		fmt.Fprintf(os.Stderr, "[acp-title-proxy] Pre-warm pool not available (%v); starting daemon in background and falling back to direct launch...\n", err)
		_ = pool.LaunchDaemonInBackground()

		// 3. Fallback direct launch
		exePath, args, err := finder.FindAGY(cfg.AGY.Executable)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[acp-title-proxy] Error: %v\n", err)
			os.Exit(1)
		}
		if len(os.Args) > 2 {
			args = append(args, os.Args[2:]...)
		}
		runner := proxy.NewRunner(exePath, args, titler)
		if err := runner.Run(inj); err != nil {
			os.Exit(1)
		}

	default:
		fmt.Fprintf(os.Stderr, "[acp-title-proxy] Unknown command: %s\n", cmd)
		printUsage()
		os.Exit(1)
	}
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
}
