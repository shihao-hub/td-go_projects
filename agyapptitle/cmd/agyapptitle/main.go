package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"language_projects/go_projects/agyapptitle/internal/buildinfo"
	"language_projects/go_projects/agyapptitle/internal/config"
	"language_projects/go_projects/agyapptitle/internal/model"
	"language_projects/go_projects/agyapptitle/internal/service"
	"language_projects/go_projects/agyapptitle/internal/store"
	"language_projects/go_projects/agyapptitle/internal/watcher"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "agyapptitle",
		Short: "Google Antigravity 会话标题智能汉化与后台守护进程",
		Long: `agyapptitle 是专为 Google Antigravity (App & CLI) 打造的本地后台守护与会话标题治理工具。
支持监听新会话落盘、调用极速大模型生成精简中文标题，并通过 ConnectRPC 实时热推送与 SQLite/Protobuf 四重原子持久化。`,
		Version: fmt.Sprintf("%s (channel: %s, buildID: %s)", buildinfo.Version, buildinfo.BuildMode, buildinfo.BuildID),
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
			// 全局 Panic 兜底防御，确保致命异常也能写入日志
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[FATAL PANIC] 守护进程发生未捕获异常: %v", r)
				}
			}()

			// 1. 第一时间确保数据目录存在并接管日志，彻底防止早期静默退出无从排查
			_ = watcher.EnsureAppDataDir()
			logPath := filepath.Join(config.GetAppDataDir(), "daemon.log")
			if logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644); err == nil {
				log.SetOutput(logFile)
			}

			// 2. 工作目录规范化：切换至可执行文件所在目录，避免开机自启等场景下 CWD 漂移
			if exePath, err := os.Executable(); err == nil {
				_ = os.Chdir(filepath.Dir(exePath))
			}

			// 3. 内核级单实例互斥锁守卫：防止计划任务与自启动项或手工启动产生多实例互踩与锁竞争
			hMutex, alreadyExists, err := watcher.AcquireWatchMutex()
			if err == nil && alreadyExists {
				log.Println("[agyapptitle] 检测到后台 watch 实例已在运行中，本实例优雅退出。")
				watcher.ReleaseWatchMutex(hMutex)
				return nil
			}
			if hMutex != 0 {
				defer watcher.ReleaseWatchMutex(hMutex)
			}

			// 4. 加载配置与状态
			cfg, err := config.LoadConfig()
			if err != nil {
				log.Printf("[启动失败] 加载配置文件异常: %v", err)
				return err
			}
			st, err := store.LoadState()
			if err != nil {
				log.Printf("[启动失败] 加载运行状态异常: %v", err)
				return err
			}

			pidFile := filepath.Join(config.GetAppDataDir(), "daemon.pid")
			_ = os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0644)
			defer os.Remove(pidFile)

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
			autostartEnabled, autostartTarget, _ := watcher.GetAutostartStatus()
			st, _ := store.LoadState()

			titledCount := 0
			var lastScan string
			if st != nil {
				titledCount = len(st.TitledSessions)
				lastScan = st.LastScanTime
			}

			if statusJSON {
				out := map[string]interface{}{
					"status":            "ok",
					"channel":           buildinfo.BuildMode,
					"app_data_dir":      config.GetAppDataDir(),
					"running":           running,
					"pid":               pid,
					"autostart_enabled": autostartEnabled,
					"autostart_target":  autostartTarget,
					"titled_count":      titledCount,
					"last_scan":         lastScan,
				}
				b, _ := json.MarshalIndent(out, "", "  ")
				fmt.Println(string(b))
				return nil
			}

			fmt.Println("\n[agyapptitle 运行状态]")
			fmt.Printf("  构建通道:   [%s]\n", buildinfo.BuildMode)
			fmt.Printf("  数据目录:   %s\n", config.GetAppDataDir())
			if running {
				fmt.Printf("  后台进程:   [运行中] (PID: %d)\n", pid)
			} else {
				fmt.Println("  后台进程:   [未运行]")
			}
			if autostartEnabled {
				fmt.Println("  开机自启:   [已开启]")
			} else {
				fmt.Println("  开机自启:   [未开启] (运行 'agyapptitle autostart enable' 开启)")
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

	// 6. autostart 命令
	autostartCmd := &cobra.Command{
		Use:   "autostart",
		Short: "管理 Windows 开机自启动设置 (enable / disable / status)",
		RunE: func(cmd *cobra.Command, args []string) error {
			enabled, target, _ := watcher.GetAutostartStatus()
			fmt.Println("\n[开机自启状态]")
			if enabled {
				fmt.Println("  状态: [已开启]")
				fmt.Printf("  命令: %s\n\n", target)
			} else {
				fmt.Println("  状态: [未开启]")
				fmt.Println("  提示: 使用 'agyapptitle autostart enable' 一键开启开机自启。")
			}
			return nil
		},
	}

	autostartEnableCmd := &cobra.Command{
		Use:   "enable",
		Short: "开启 Windows 开机自启动（自动绑定无黑框版本 agyapptitlew.exe）",
		RunE: func(cmd *cobra.Command, args []string) error {
			cmdLine, err := watcher.EnableAutostart()
			if err != nil {
				return err
			}
			fmt.Println("[成功] 已开启 Windows 开机自启动！")
			fmt.Printf("注册命令: %s\n", cmdLine)
			return nil
		},
	}

	autostartDisableCmd := &cobra.Command{
		Use:   "disable",
		Short: "关闭 Windows 开机自启动",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := watcher.DisableAutostart(); err != nil {
				return err
			}
			fmt.Println("[成功] 已关闭 Windows 开机自启动。")
			return nil
		},
	}

	autostartCmd.AddCommand(autostartEnableCmd, autostartDisableCmd)

	// 7. schema 命令
	schemaCmd := &cobra.Command{
		Use:   "schema",
		Short: "零 I/O 极速导出输出数据的 JSON Schema 契约",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println(model.ExportSchema())
		},
	}

	rootCmd.AddCommand(syncCmd, watchCmd, startCmd, stopCmd, statusCmd, autostartCmd, schemaCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
