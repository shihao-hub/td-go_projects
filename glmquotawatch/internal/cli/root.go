// Package cli 是 glmquotawatch 的 CLI 薄壳（cobra）：
// 每个命令只做「解析 → service → 输出 → 退出码」，业务规则全部在 service 层。
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"glmquotawatch/internal/mcp"
	"glmquotawatch/internal/notify"
	"glmquotawatch/internal/service"

	"github.com/spf13/cobra"
)

// Version 版本号，构建时经 -ldflags 注入。
var Version = "dev"

// mustService 惰性组装业务服务（help/version/schema 不触发数据目录创建之外的开销）。
var (
	svcOnce sync.Once
	svcInst *service.Service
	svcErr  error
)

func mustService() (*service.Service, error) {
	svcOnce.Do(func() {
		svcInst, svcErr = service.Open()
	})
	return svcInst, svcErr
}

// Run 执行 CLI 并返回进程退出码：0 成功、1 业务失败、2 参数/flag 错误。
func Run(args []string) int {
	root := newRootCmd()
	root.SetArgs(args) // 不传则 cobra 默认读 os.Args[1:]，进程内调用（测试）会错乱
	err := root.ExecuteContext(context.Background())
	if err == nil {
		return 0
	}
	var ce *codedError
	if errors.As(err, &ce) {
		return 1 // 错误输出已在命令内完成
	}
	// cobra 的 flag/参数错误：人读走 stderr，--json 时信封到 stdout
	se := &service.Error{Code: "bad_args", Message: err.Error()}
	if jsonMode(root) {
		emitErrJSON(os.Stdout, se)
	} else {
		fmt.Fprintf(os.Stderr, "错误: %s\n", se.Message)
	}
	return 2
}

func newRootCmd() *cobra.Command {
	mcp.Version = Version
	root := &cobra.Command{
		Use:     "glmquotawatch",
		Short:   "GLM 编码套餐用量采样与阈值告警（Windows Toast）",
		Version: Version,
		Long: "定时采样智谱 GLM（bigmodel）编码套餐用量，跨越阈值时弹 Windows Toast 通知。\n" +
			"先执行 token set 配置密钥，再 start 启动常驻监控；status 可手动采样一次。",
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	root.PersistentFlags().Bool("json", false, "以 JSON 信封输出（供脚本/程序消费）")
	root.SetVersionTemplate("{{.Version}}\n")
	root.AddCommand(
		tokenCmd(),
		statusCmd(),
		configCmd(),
		startCmd(),
		mcpCmd(),
		schemaCmd(),
	)
	return root
}

// ---- token ----

func tokenCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "token",
		Short: "配置 GLM API token",
	}
	set := &cobra.Command{
		Use:   "set <token>",
		Short: "设置 token（bigmodel 开放平台密钥，长度 ≥ 20）",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := mustService()
			if err != nil {
				return outErr(cmd, err)
			}
			view, err := svc.SetToken(args[0])
			if err != nil {
				return outErr(cmd, err)
			}
			return outOK(cmd, view)
		},
	}
	show := &cobra.Command{
		Use:   "show",
		Short: "查看 token（脱敏）",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := mustService()
			if err != nil {
				return outErr(cmd, err)
			}
			view, err := svc.GetConfig()
			if err != nil {
				return outErr(cmd, err)
			}
			return outOK(cmd, view)
		},
	}
	remove := &cobra.Command{
		Use:   "remove",
		Short: "清除 token（幂等）",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := mustService()
			if err != nil {
				return outErr(cmd, err)
			}
			view, err := svc.RemoveToken()
			if err != nil {
				return outErr(cmd, err)
			}
			return outOK(cmd, view)
		},
	}
	cmd.AddCommand(set, show, remove)
	return cmd
}

// ---- status ----

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "立即采样一次用量（落历史并推进告警状态，不发通知）",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := mustService()
			if err != nil {
				return outErr(cmd, err)
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()
			view, _, err := svc.SampleOnce(ctx)
			if err != nil {
				return outErr(cmd, err)
			}
			return outOK(cmd, view)
		},
	}
}

// ---- config ----

func configCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "查看或修改采样配置",
	}
	show := &cobra.Command{
		Use:   "show",
		Short: "查看全部配置与数据目录",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := mustService()
			if err != nil {
				return outErr(cmd, err)
			}
			view, err := svc.GetConfig()
			if err != nil {
				return outErr(cmd, err)
			}
			return outOK(cmd, view)
		},
	}
	set := &cobra.Command{
		Use:   "set <key> <value>",
		Short: "修改单个配置键（interval / thresholds / hysteresis / silent）",
		Args:  cobra.ExactArgs(2),
		Example: `  glmquotawatch config set interval 90s
  glmquotawatch config set thresholds 50,60,80,90
  glmquotawatch config set silent true`,
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := mustService()
			if err != nil {
				return outErr(cmd, err)
			}
			view, err := svc.SetConfig(args[0], args[1])
			if err != nil {
				return outErr(cmd, err)
			}
			return outOK(cmd, view)
		},
	}
	cmd.AddCommand(show, set)
	return cmd
}

// ---- start ----

func startCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "启动常驻监控：定时采样 + 跨阈值弹 Windows Toast（Ctrl+C 停止）",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := mustService()
			if err != nil {
				return outErr(cmd, err)
			}
			cfg, err := svc.GetConfig()
			if err != nil {
				return outErr(cmd, err)
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
			notifier := notify.Toast{Silent: cfg.Silent}
			if err := svc.RunDaemon(ctx, notifier, logger); err != nil {
				return outErr(cmd, err)
			}
			return nil
		},
	}
}

// ---- mcp ----

func mcpCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "以 stdio MCP server 运行（供 AI 客户端接入）",
		RunE: func(cmd *cobra.Command, args []string) error {
			return mcp.Run(cmd.Context())
		},
	}
}

// ---- schema ----

func schemaCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "schema",
		Short: "导出与 tools/list 同源的 MCP 工具定义（离线检查用，输出即 JSON）",
		RunE: func(cmd *cobra.Command, args []string) error {
			tools, err := mcp.Schema(cmd.Context())
			if err != nil {
				return outErr(cmd, err)
			}
			b, err := json.MarshalIndent(map[string]any{
				"name":    "glmquotawatch",
				"version": Version,
				"tools":   tools,
			}, "", "  ")
			if err != nil {
				return outErr(cmd, err)
			}
			fmt.Println(string(b))
			return nil
		},
	}
}
