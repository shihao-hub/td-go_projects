// Package cli 是 agyquota 的 CLI 薄壳（cobra）：
// 命令只做「解析 → service → 输出 → 退出码」，业务规则全部在 service 层。
package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"agyquota/internal/mcp"
	"agyquota/internal/service"

	"github.com/spf13/cobra"
)

// Version 版本号，构建时经 -ldflags 注入。
var Version = "0.2.3"

// Run 执行 CLI 并返回进程退出码：0 成功、1 业务失败、2 参数/flag 错误。
func Run(args []string) int {
	mcp.Version = Version
	root := newRootCmd()
	root.SetArgs(args)
	err := root.Execute()
	if err == nil {
		return 0
	}
	var berr *service.Error
	if errors.As(err, &berr) {
		return 1 // 错误输出已在命令内完成
	}
	// cobra 的 flag/参数错误：人读走 stderr，--json 时信封到 stdout
	se := &service.Error{Code: "bad_args", Message: err.Error()}
	if jsonMode(root) {
		emitErrJSON(root.OutOrStdout(), se)
	} else {
		fmt.Fprintf(os.Stderr, "错误: %s\n", se.Message)
	}
	return 2
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "agyquota",
		Short: "Antigravity 模型配额查询（需指定 --agy 或 --zed）",
		Long: "查询 Google AI Pro 在 Antigravity 中的模型配额（Gemini / Claude and GPT）。\n\n" +
			"数据源选项（二选一）：\n" +
			"  --agy : 调用官方 Antigravity CLI (agy)，查询终端与桌面端账号配额，调用后自动回收进程；\n" +
			"  --zed : 读取 Zed (antigravity-acp) 本地凭据直连 Google 官方接口，支持本地安全缓存。\n\n" +
			"不带子命令运行时等价于 agyquota quota。",
		Version:       Version,
		SilenceErrors: true,
		SilenceUsage:  true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return quotaRun(cmd)
		},
	}
	root.PersistentFlags().Bool("agy", false, "查询官方 Antigravity CLI / 桌面端账号配额（进程随用随杀）")
	root.PersistentFlags().Bool("zed", false, "查询 Zed (antigravity-acp) 账号配额（带 50 分钟安全缓存）")
	root.PersistentFlags().Bool("json", false, "以 JSON 信封输出（供脚本/程序消费）")
	root.PersistentFlags().String("token-file", "", "Zed 凭据文件路径（仅 --zed 生效，缺省用 ~/.gemini/antigravity-acp/acp_token.json）")
	root.PersistentFlags().Bool("raw", false, "输出配额接口原始响应（调试用，仅 quota 生效）")
	root.SetVersionTemplate("{{.Version}}\n")
	root.AddCommand(quotaCmd(), mcpCmd(), schemaCmd())
	return root
}

// ---- mcp ----

// mcpCmd 以 stdio MCP server 运行（协议走 stdin/stdout，日志走 stderr）。
func mcpCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "以 stdio MCP server 运行（供 AI 客户端接入）",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return mcp.Run(cmd.Context())
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
				"version": Version,
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
