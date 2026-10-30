package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"agysess/internal/model"
	"agysess/internal/service"

	"github.com/spf13/cobra"
)

// RootOptions 全局命令行参数
type RootOptions struct {
	JSON       bool
	Schema     bool
	CustomDB   string
	CustomMeta string
}

// BuildRootCmd 构造 CLI 根命令与完整子命令树
func BuildRootCmd(version string, runGUIFunc func(svc *service.SessionService) error) (*cobra.Command, *service.SessionService) {
	opts := &RootOptions{}
	var svc *service.SessionService

	rootCmd := &cobra.Command{
		Use:   "agysess",
		Short: "Antigravity (agy) CLI 会话管理与 Windows Terminal 联动工具",
		Long: `agysess 是一款专为 Google Antigravity agy CLI 设计的轻量级会话管理与终端联动工具。
提供会话列表多维查询、归档排序、Windows Terminal 一键唤起与运行中窗口回收等功能。`,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			// 零 I/O 检查：若传入 --schema，立即输出 Schema 并静默拦截后续业务 I/O
			if opts.Schema || cmd.Name() == "schema" {
				return nil
			}

			// 延迟初始化 Service，保证离线 Schema 导出零 I/O
			var err error
			svc, err = service.NewSessionService(opts.CustomDB, opts.CustomMeta)
			if err != nil {
				return fmt.Errorf("初始化核心服务失败: %w", err)
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if opts.Schema {
				data, err := model.GetStaticSchema(version)
				if err != nil {
					return err
				}
				fmt.Println(string(data))
				return nil
			}
			// 默认无参数行为：若支持 GUI 则唤起 GUI，否则打印帮助
			if runGUIFunc != nil {
				return runGUIFunc(svc)
			}
			return cmd.Help()
		},
	}

	rootCmd.PersistentFlags().BoolVar(&opts.JSON, "json", false, "输出紧凑稳定的机器 JSON 包络")
	rootCmd.PersistentFlags().BoolVar(&opts.Schema, "schema", false, "导出离线 JSON Schema 契约（零 I/O 极速反射）")
	rootCmd.PersistentFlags().StringVar(&opts.CustomDB, "db-path", "", "指定自定义 Agy 会话 SQLite 数据库路径")
	rootCmd.PersistentFlags().StringVar(&opts.CustomMeta, "meta-path", "", "指定自定义元数据 JSON 路径")

	// 1. schema 命令（显式子命令）
	schemaCmd := &cobra.Command{
		Use:   "schema",
		Short: "导出离线契约元数据（零 I/O 极速反射）",
		RunE: func(cmd *cobra.Command, args []string) error {
			data, err := model.GetStaticSchema(version)
			if err != nil {
				return err
			}
			fmt.Println(string(data))
			return nil
		},
	}
	rootCmd.AddCommand(schemaCmd)

	// 2. list 命令
	var (
		listQuery    string
		listProject  string
		listAll      bool
		listArchived bool
		listSort     string
		listLimit    int
	)
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "查询并过滤会话列表",
		RunE: func(cmd *cobra.Command, args []string) error {
			if opts.Schema {
				data, err := model.GetStaticSchema(version)
				if err != nil {
					return err
				}
				fmt.Println(string(data))
				return nil
			}

			filterOpts := model.FilterOptions{
				Query:           listQuery,
				ProjectID:       listProject,
				IncludeArchived: listAll,
				ArchivedOnly:    listArchived,
				SortBy:          listSort,
				Limit:           listLimit,
			}

			sessions, stats, err := svc.ListSessions(cmd.Context(), filterOpts)
			if err != nil {
				if opts.JSON {
					PrintJSONError(os.Stdout, "query_failed", err.Error())
				}
				return err
			}

			if opts.JSON {
				PrintJSON(os.Stdout, map[string]interface{}{
					"items": sessions,
					"total": len(sessions),
					"stats": stats,
				})
			} else {
				RenderSessionTable(os.Stdout, sessions, stats)
			}
			return nil
		},
	}
	listCmd.Flags().StringVarP(&listQuery, "query", "q", "", "按标题、预览内容或 ID 过滤")
	listCmd.Flags().StringVarP(&listProject, "project", "p", "", "按所属项目筛选")
	listCmd.Flags().BoolVarP(&listAll, "all", "a", false, "显示所有会话（包括已归档）")
	listCmd.Flags().BoolVar(&listArchived, "archived", false, "仅显示已归档会话")
	listCmd.Flags().StringVar(&listSort, "sort", "time_desc", "排序字段 (time_desc, time_asc, steps_desc, steps_asc, title_asc)")
	listCmd.Flags().IntVar(&listLimit, "limit", 0, "限制返回条目数量")
	rootCmd.AddCommand(listCmd)

	// 3. show 命令
	showCmd := &cobra.Command{
		Use:   "show <session-id>",
		Short: "查看单个会话详细信息",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if opts.Schema {
				data, err := model.GetStaticSchema(version)
				if err != nil {
					return err
				}
				fmt.Println(string(data))
				return nil
			}

			sessionID := args[0]
			sess, err := svc.GetSession(cmd.Context(), sessionID)
			if err != nil {
				if opts.JSON {
					PrintJSONError(os.Stdout, "session_not_found", err.Error())
				}
				return err
			}

			if opts.JSON {
				PrintJSON(os.Stdout, sess)
			} else {
				RenderSessionDetail(os.Stdout, sess)
			}
			return nil
		},
	}
	rootCmd.AddCommand(showCmd)

	// 4. archive 命令
	archiveCmd := &cobra.Command{
		Use:   "archive <session-id>",
		Short: "归档指定会话",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sessionID := args[0]
			if err := svc.ArchiveSession(cmd.Context(), sessionID); err != nil {
				if opts.JSON {
					PrintJSONError(os.Stdout, "archive_failed", err.Error())
				}
				return err
			}
			if opts.JSON {
				PrintJSON(os.Stdout, map[string]interface{}{"id": sessionID, "archived": true})
			} else {
				fmt.Printf("已成功归档会话 [%s]\n", sessionID)
			}
			return nil
		},
	}
	rootCmd.AddCommand(archiveCmd)

	// 5. unarchive 命令
	unarchiveCmd := &cobra.Command{
		Use:   "unarchive <session-id>",
		Short: "取消归档指定会话",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sessionID := args[0]
			if err := svc.UnarchiveSession(cmd.Context(), sessionID); err != nil {
				if opts.JSON {
					PrintJSONError(os.Stdout, "unarchive_failed", err.Error())
				}
				return err
			}
			if opts.JSON {
				PrintJSON(os.Stdout, map[string]interface{}{"id": sessionID, "archived": false})
			} else {
				fmt.Printf("已恢复会话 [%s] 至活跃状态\n", sessionID)
			}
			return nil
		},
	}
	rootCmd.AddCommand(unarchiveCmd)

	// 6. rename 命令
	renameCmd := &cobra.Command{
		Use:   "rename <session-id> <new-title>",
		Short: "设置会话自定义标题",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			sessionID, newTitle := args[0], args[1]
			if err := svc.UpdateCustomTitle(cmd.Context(), sessionID, newTitle); err != nil {
				if opts.JSON {
					PrintJSONError(os.Stdout, "rename_failed", err.Error())
				}
				return err
			}
			if opts.JSON {
				PrintJSON(os.Stdout, map[string]interface{}{"id": sessionID, "custom_title": newTitle})
			} else {
				fmt.Printf("已更新会话 [%s] 标题为: %s\n", sessionID, newTitle)
			}
			return nil
		},
	}
	rootCmd.AddCommand(renameCmd)

	// 7. note 命令
	noteCmd := &cobra.Command{
		Use:   "note <session-id> <notes>",
		Short: "更新会话备注信息",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			sessionID, notes := args[0], args[1]
			if err := svc.UpdateNotes(cmd.Context(), sessionID, notes); err != nil {
				if opts.JSON {
					PrintJSONError(os.Stdout, "note_failed", err.Error())
				}
				return err
			}
			if opts.JSON {
				PrintJSON(os.Stdout, map[string]interface{}{"id": sessionID, "notes": notes})
			} else {
				fmt.Printf("已更新会话 [%s] 备注\n", sessionID)
			}
			return nil
		},
	}
	rootCmd.AddCommand(noteCmd)

	// 8. open 命令
	var openNewWindow bool
	openCmd := &cobra.Command{
		Use:   "open <session-id>",
		Short: "在 Windows Terminal 中打开/恢复指定会话",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sessionID := args[0]
			if err := svc.OpenSession(cmd.Context(), sessionID, openNewWindow); err != nil {
				if opts.JSON {
					PrintJSONError(os.Stdout, "open_failed", err.Error())
				}
				return err
			}
			if opts.JSON {
				PrintJSON(os.Stdout, map[string]interface{}{"id": sessionID, "opened": true, "new_window": openNewWindow})
			} else {
				fmt.Printf("已在 Windows Terminal 中唤起会话 [%s]\n", sessionID)
			}
			return nil
		},
	}
	openCmd.Flags().BoolVarP(&openNewWindow, "new-window", "w", false, "在新独立窗口中打开，而非新建标签页")
	rootCmd.AddCommand(openCmd)

	// 9. new 命令
	var (
		newDir      string
		newProject  string
		newModel    string
		newMode     string
		newEffort   string
		newPrompt   string
		newInWindow bool
	)
	newCmd := &cobra.Command{
		Use:   "new",
		Short: "在 Windows Terminal 中新建 agy 会话",
		RunE: func(cmd *cobra.Command, args []string) error {
			createOpts := model.CreateSessionOptions{
				WorkspaceDir:  newDir,
				ProjectID:     newProject,
				Model:         newModel,
				Mode:          newMode,
				Effort:        newEffort,
				InitialPrompt: newPrompt,
				NewWindow:     newInWindow,
			}
			if err := svc.CreateSession(cmd.Context(), createOpts); err != nil {
				if opts.JSON {
					PrintJSONError(os.Stdout, "create_failed", err.Error())
				}
				return err
			}
			if opts.JSON {
				PrintJSON(os.Stdout, map[string]interface{}{"created": true, "options": createOpts})
			} else {
				fmt.Println("已成功在 Windows Terminal 中启动新建会话")
			}
			return nil
		},
	}
	newCmd.Flags().StringVarP(&newDir, "dir", "d", "", "会话工作区目录路径")
	newCmd.Flags().StringVarP(&newProject, "project", "p", "", "指定项目 ID 或名称")
	newCmd.Flags().StringVarP(&newModel, "model", "m", "", "指定模型")
	newCmd.Flags().StringVar(&newMode, "mode", "", "代理模式 (accept-edits, auto, plan)")
	newCmd.Flags().StringVar(&newEffort, "effort", "", "思考强度 (low, medium, high, xhigh, max)")
	newCmd.Flags().StringVarP(&newPrompt, "prompt", "i", "", "初始输入提示词")
	newCmd.Flags().BoolVarP(&newInWindow, "new-window", "w", false, "在新独立窗口中拉起")
	rootCmd.AddCommand(newCmd)

	// 10. running 命令
	runningCmd := &cobra.Command{
		Use:   "running",
		Short: "探测系统当前活跃运行中的 agy 进程",
		RunE: func(cmd *cobra.Command, args []string) error {
			running, err := svc.GetRunningSessions(cmd.Context())
			if err != nil {
				if opts.JSON {
					PrintJSONError(os.Stdout, "detect_failed", err.Error())
				}
				return err
			}
			if opts.JSON {
				PrintJSON(os.Stdout, running)
			} else {
				if len(running) == 0 {
					fmt.Println("当前未检测到活跃运行的 agy 会话。")
				} else {
					fmt.Printf("%-8s  %-36s  %s\n", "PID", "关联会话 ID", "命令行详情")
					fmt.Println(strings.Repeat("-", 80))
					for _, r := range running {
						sid := r.SessionID
						if sid == "" {
							sid = "(未绑定特定会话ID)"
						}
						fmt.Printf("%-8d  %-36s  %s\n", r.PID, sid, TruncateString(r.CommandLine, 40))
					}
					fmt.Printf("\n共 %d 个活跃 agy 进程\n", len(running))
				}
			}
			return nil
		},
	}
	rootCmd.AddCommand(runningCmd)

	// 11. kill 命令
	killCmd := &cobra.Command{
		Use:   "kill <session-id>",
		Short: "关闭指定会话的 Windows Terminal 窗口/进程",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sessionID := args[0]
			pid, err := svc.KillSession(cmd.Context(), sessionID)
			if err != nil {
				if opts.JSON {
					PrintJSONError(os.Stdout, "kill_failed", err.Error())
				}
				return err
			}
			if opts.JSON {
				PrintJSON(os.Stdout, map[string]interface{}{"id": sessionID, "killed_pid": pid, "status": "terminated"})
			} else {
				fmt.Printf("已成功关闭会话 [%s] 对应的终端进程 (PID: %d)\n", sessionID, pid)
			}
			return nil
		},
	}
	rootCmd.AddCommand(killCmd)

	// 12. gui 命令
	guiCmd := &cobra.Command{
		Use:   "gui",
		Short: "启动 Fyne 轻量原生桌面管理面板",
		RunE: func(cmd *cobra.Command, args []string) error {
			if runGUIFunc == nil {
				return fmt.Errorf("当前构建未启用 GUI 支持")
			}
			return runGUIFunc(svc)
		},
	}
	rootCmd.AddCommand(guiCmd)

	return rootCmd, svc
}

// Execute 执行 CLI 入口并映射标准退出码 (0: 成功, 2: 参数错误, 1: 业务失败)
func Execute(version string, runGUIFunc func(svc *service.SessionService) error) int {
	rootCmd, _ := BuildRootCmd(version, runGUIFunc)
	ctx := context.Background()

	err := rootCmd.ExecuteContext(ctx)
	if err == nil {
		return 0
	}

	// 判断是否为参数解析错误
	// 若是未知 flag、缺少参数等则为 2；业务执行失败为 1
	fmt.Fprintln(os.Stderr, "错误:", err)
	return 1
}
