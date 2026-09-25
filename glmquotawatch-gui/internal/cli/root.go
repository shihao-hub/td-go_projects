// Package cli 是 glmquotawatch-gui 的机器友好薄壳（cobra）：
// 所有命令恒输出 JSON 信封（面向脚本/AI，不提供人读模式）；
// 每个命令只做「解析 → service → 信封输出 → 退出码」，
// 业务规则全部在 service 层。GUI 是面向人的入口。
package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"glmquotawatch-gui/internal/service"
	"glmquotawatch-gui/internal/store"

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
		dir, err := store.DefaultDir()
		if err != nil {
			svcErr = err
			return
		}
		svcInst, svcErr = service.Open(dir)
	})
	return svcInst, svcErr
}

// Run 执行 CLI 并返回进程退出码：0 成功、1 业务失败、2 参数/flag 错误。
// help/version 同样以 JSON 信封输出（stdout 恒纯 JSON）。
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
	// cobra 的 flag/参数错误：信封到 stdout（机器侧统一消费），退出码 2
	se := &service.Error{Code: "bad_args", Message: err.Error()}
	emitErrJSON(os.Stdout, se)
	return 2
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:     "glmquotawatch-gui",
		Short:   "GLM 编码套餐用量采样与阈值告警（GUI 工具的机器友好 CLI 壳，恒 JSON 输出）",
		Version: Version,
		Long: "glmquotawatch-gui 的 CLI 壳面向脚本与 AI 消费：所有命令 stdout 恒输出 JSON 信封\n" +
			"{\"ok\":true,\"data\":…} / {\"ok\":false,\"error\":{\"code\",\"message\"}}，退出码 0/1/2。\n" +
			"面向人的入口是 GUI（无参数启动本程序）。",
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	// --help 与 --version 同样信封化：stdout 纯 JSON 不变式对元命令也成立
	root.SetHelpFunc(envelopeHelp)
	root.SetVersionTemplate(`{"ok": true, "data": {"version": "{{.Version}}"}}` + "\n")
	root.AddCommand(
		tokenCmd(),
		statusCmd(),
		configCmd(),
		schemaCmd(),
		versionCmd(),
	)
	return root
}

// envelopeHelp 以 JSON 信封输出 usage（含子命令清单与说明）。
func envelopeHelp(cmd *cobra.Command, _ []string) {
	emitJSON(os.Stdout, map[string]any{"usage": cmd.UsageString()})
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
		Example: `  glmquotawatch-gui config set interval 90s
  glmquotawatch-gui config set thresholds 50,60,80,90
  glmquotawatch-gui config set silent true`,
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

// ---- schema ----

func schemaCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "schema",
		Short: "导出本 CLI 自身的命令契约目录（interface: cli，零业务 I/O）",
		Long: "输出本 CLI 的命令/参数/退出码契约 JSON（静态生成，不读配置、不建目录、不联网）。\n" +
			"本项目未提供 MCP server（用户决策），本目录即机器侧的契约自描述。",
		RunE: func(cmd *cobra.Command, args []string) error {
			doc, err := BuildSchemaDoc()
			if err != nil {
				return outErr(cmd, err)
			}
			b, err := marshalIndent(doc)
			if err != nil {
				return outErr(cmd, err)
			}
			fmt.Fprintln(os.Stdout, string(b))
			return nil
		},
	}
}

// ---- version ----

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "输出版本号（JSON 信封）",
		RunE: func(cmd *cobra.Command, args []string) error {
			return outOK(cmd, map[string]string{"version": Version})
		},
	}
}
