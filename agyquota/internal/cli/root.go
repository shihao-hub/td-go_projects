// Package cli 是 agyquota 的 CLI 薄壳（cobra）：
// 命令只做「解析 → HTTP client → 渲染 → 退出码」，
// 业务规则全部在 daemon 内；本包不 import internal/service（依赖方向硬约束）。
package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"agyquota/internal/api"
	"agyquota/internal/buildinfo"
	"agyquota/internal/client"
	"agyquota/internal/mcp"

	"github.com/spf13/cobra"
)

// NewRootCmd 构造根命令：quota（默认）/ mcp / schema / stop。
// serve 由 cmd/agyquota 用 daemon.NewServeCmd() 挂载。
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "agyquota",
		Short: "Antigravity 模型配额查询（需指定 --agy 或 --zed）",
		Long: "查询 Google AI Pro 在 Antigravity 中的模型配额（Gemini / Claude and GPT）。\n\n" +
			"数据源选项（二选一）：\n" +
			"  --agy : 调用官方 Antigravity CLI (agy)，查询终端与桌面端账号配额，调用后自动回收进程；\n" +
			"  --zed : 读取 Zed (antigravity-acp) 本地凭据直连 Google 官方接口，支持本地安全缓存。\n\n" +
			"查询由后台 daemon 执行（agyquota serve）：生产构建会自动拉起并空闲 30 分钟自动退出；\n" +
			"开发构建请先运行 agyquota serve（双终端工作流）。\n\n" +
			"不带子命令运行时等价于 agyquota quota。",
		Version:       buildinfo.Version,
		SilenceErrors: true,
		SilenceUsage:  true,
		Args:          cobra.NoArgs,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			host, err := cmd.Flags().GetString("host")
			if err == nil && host != "" {
				if _, err := client.NormalizeHost(host); err != nil {
					return fmt.Errorf("--host 参数无效: %v", err)
				}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return quotaRun(cmd)
		},
	}
	root.PersistentFlags().Bool("agy", false, "查询官方 Antigravity CLI / 桌面端账号配额（进程随用随杀）")
	root.PersistentFlags().Bool("zed", false, "查询 Zed (antigravity-acp) 账号配额（带 50 分钟安全缓存）")
	root.PersistentFlags().Bool("json", false, "以 JSON 信封输出（供脚本/程序消费）")
	root.PersistentFlags().String("token-file", "", "Zed 凭据文件路径（仅 --zed 生效，缺省用 ~/.gemini/antigravity-acp/acp_token.json）")
	root.PersistentFlags().Bool("raw", false, "输出配额接口原始响应（调试用，仅 quota 生效）")
	root.PersistentFlags().String("host", "", "daemon 目标地址 host[:port]（缺省端口 17625；默认按 AGYQUOTA_HOST/地址文件自动发现）")
	root.SetVersionTemplate("{{.Version}}\n")
	root.AddCommand(quotaCmd(), mcpCmd(), schemaCmd(), stopCmd())
	return root
}

// Execute 执行 CLI 并返回进程退出码：0 成功、1 业务失败、2 参数/flag 错误。
func Execute(root *cobra.Command, args []string) int {
	root.SetArgs(args)
	err := root.Execute()
	if err == nil {
		return 0
	}
	var berr *api.Error
	if errors.As(err, &berr) {
		return 1 // 错误输出已在命令内完成
	}
	// cobra 的 flag/参数错误：人读走 stderr，--json 时信封到 stdout
	se := &api.Error{Code: api.ErrBadArgs, Message: err.Error()}
	if jsonMode(root) {
		emitErrJSON(root.OutOrStdout(), se)
	} else {
		fmt.Fprintf(os.Stderr, "错误: %s\n", se.Message)
	}
	return 2
}

// ---- mcp ----

// mcpCmd 以 stdio MCP server 运行（协议走 stdin/stdout，日志走 stderr）。
// --host 显式下传给桥，不允许静默忽略。
func mcpCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "以 stdio MCP server 运行（供 AI 客户端接入）",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			host, _ := cmd.Flags().GetString("host")
			return mcp.Run(cmd.Context(), client.Config{Host: host})
		},
	}
}

// ---- schema ----

// schemaCmd 导出与 tools/list 同源的 MCP 工具定义（离线检查用，输出即 JSON）。
func schemaCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "schema",
		Short: "导出与 tools/list 同源的 MCP 工具定义（离线检查用，输出即 JSON）",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			tools, err := mcp.Schema(cmd.Context())
			if err != nil {
				return outErr(cmd, err)
			}
			b, err := json.MarshalIndent(map[string]any{
				"name":    "agyquota",
				"version": buildinfo.Version,
				"tools":   tools,
			}, "", "  ")
			if err != nil {
				return outErr(cmd, err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), string(b))
			return nil
		},
	}
}

// ---- stop ----

// stopCmd 请求 daemon 优雅退出并清理地址文件；未运行按成功 no-op。
func stopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "停止正在运行的 agyquota daemon（未运行时不报错）",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			host, _ := cmd.Flags().GetString("host")
			cl := client.New(client.Config{Host: host})
			ctx, cancel := contextTimeout(cmd, 10*time.Second)
			defer cancel()
			stopped, err := cl.Stop(ctx)
			if err != nil {
				return outErr(cmd, err)
			}
			if jsonMode(cmd) {
				emitOK(cmd.OutOrStdout(), api.StopResponse{Stopped: stopped})
				return nil
			}
			if stopped {
				fmt.Fprintln(cmd.OutOrStdout(), "daemon 已停止")
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "daemon 未运行")
			}
			return nil
		},
	}
}
