package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"language_projects/go_projects/agyapptitle/internal/config"
	"language_projects/go_projects/agyapptitle/internal/model"
	"language_projects/go_projects/agyapptitle/internal/service"
	"language_projects/go_projects/agyapptitle/internal/store"
	"language_projects/go_projects/agyapptitle/internal/watcher"
)

var (
	version = "1.0.0"
	commit  = "none"
	date    = "unknown"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "agyapptitle",
		Short: "Google Antigravity 会话标题智能汉化与后台守护进程",
		Long: `agyapptitle 是专为 Google Antigravity (App & CLI) 打造的本地后台守护与会话标题治理工具。
支持监听新会话落盘、调用极速大模型生成精简中文标题，并通过 ConnectRPC 实时热推送与 SQLite/Protobuf 四重原子持久化。`,
		Version: fmt.Sprintf("%s (commit: %s, date: %s)", version, commit, date),
	}

	// 1. sync 命令
	var dryRun bool
	var target string
	var jsonOutput bool

	syncCmd := &cobra.Command{
		Use:   "sync",
		Short: "执行单次会话扫描并汉化英文标题",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadConfig()
			if err != nil {
				return err
			}
			st, err := store.LoadState()
			if err != nil {
				return err
			}

			svc := service.NewService(cfg, st)
			tasks, err := svc.SyncOnce(target, dryRun)
			if err != nil {
				return err
			}

			if jsonOutput {
				out := map[string]interface{}{
					"status":  "ok",
					"dry_run": dryRun,
					"count":   len(tasks),
					"tasks":   tasks,
				}
				b, _ := json.MarshalIndent(out, "", "  ")
				fmt.Println(string(b))
				return nil
			}

			fmt.Printf("\n[agyapptitle] 扫描与同步完成 (dry-run: %v)\n", dryRun)
			if len(tasks) == 0 {
				fmt.Println("未发现需要汉化的英文会话。")
				return nil
			}

			fmt.Println("处理会话清单：")
			fmt.Println("--------------------------------------------------------------------------------")
			for _, t := range tasks {
				fmt.Printf("[%s] %s: %s -> %s\n", t.Env, t.SessionID[:8], t.OldTitle, t.NewTitle)
			}
			fmt.Printf("--------------------------------------------------------------------------------\n共处理 %d 条会话。\n\n", len(tasks))
			return nil
		},
	}
	syncCmd.Flags().BoolVar(&dryRun, "dry-run", false, "预览模式，不实际写入数据库")
	syncCmd.Flags().StringVarP(&target, "target", "t", "all", "目标环境: all, app, cli")
	syncCmd.Flags().BoolVar(&jsonOutput, "json", false, "输出标准 JSON 格式")

	// 2. watch 命令
	watchCmd := &cobra.Command{
		Use:   "watch",
		Short: "在前台以监听模式运行，持续捕获新会话并自动汉化",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadConfig()
			if err != nil {
				return err
			}
			st, err := store.LoadState()
			if err != nil {
				return err
			}

			svc := service.NewService(cfg, st)
			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer cancel()

			interval := time.Duration(cfg.PollIntervalSec) * time.Second
			if interval <= 0 {
				interval = 4 * time.Second
			}

			svc.Watch(ctx, interval)
			return nil
		},
	}

	// 3. start 命令
	startCmd := &cobra.Command{
		Use:   "start",
		Short: "在 Windows 后台以零黑框静默模式拉起守护进程",
		RunE: func(cmd *cobra.Command, args []string) error {
			pid, err := watcher.StartDaemon()
			if err != nil {
				return err
			}
			fmt.Printf("[成功] agyapptitle 守护进程已在后台启动 (PID: %d)\n", pid)
			return nil
		},
	}

	// 4. stop 命令
	stopCmd := &cobra.Command{
		Use:   "stop",
		Short: "停止正在后台运行的守护进程",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := watcher.StopDaemon(); err != nil {
				return err
			}
			fmt.Println("[成功] agyapptitle 守护进程已停止。")
			return nil
		},
	}

	// 5. status 命令
	var statusJSON bool
	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "查看后台守护进程与会话汉化状态",
		RunE: func(cmd *cobra.Command, args []string) error {
			running, pid := watcher.CheckDaemonRunning()
			st, _ := store.LoadState()

			titledCount := 0
			var lastScan string
			if st != nil {
				titledCount = len(st.TitledSessions)
				lastScan = st.LastScanTime
			}

			if statusJSON {
				out := map[string]interface{}{
					"status":       "ok",
					"running":      running,
					"pid":          pid,
					"titled_count": titledCount,
					"last_scan":    lastScan,
				}
				b, _ := json.MarshalIndent(out, "", "  ")
				fmt.Println(string(b))
				return nil
			}

			fmt.Println("\n[agyapptitle 运行状态]")
			if running {
				fmt.Printf("  后台进程:   [运行中] (PID: %d)\n", pid)
			} else {
				fmt.Println("  后台进程:   [未运行]")
			}
			fmt.Printf("  已汉化会话: %d 个\n", titledCount)
			if lastScan != "" {
				fmt.Printf("  最近活跃:   %s\n", lastScan)
			}
			fmt.Println()
			return nil
		},
	}
	statusCmd.Flags().BoolVar(&statusJSON, "json", false, "输出标准 JSON 格式")

	// 6. schema 命令
	schemaCmd := &cobra.Command{
		Use:   "schema",
		Short: "零 I/O 极速导出输出数据的 JSON Schema 契约",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println(model.ExportSchema())
		},
	}

	rootCmd.AddCommand(syncCmd, watchCmd, startCmd, stopCmd, statusCmd, schemaCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
