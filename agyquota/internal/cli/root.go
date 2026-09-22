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
var Version = "0.1.0"

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
		Use:     "agyquota",
		Short:   "Antigravity 模型配额查询（无需打开 IDE）",
		Long: "通过 Antigravity 落盘的 Google OAuth 凭据直接查询 Cloud Code 配额接口，\n" +
			"显示各模型桶（Gemini / Claude and GPT）的剩余百分比与重置时间。\n" +
			"凭据来源：~\\.gemini\\antigravity-acp\\acp_token.json（只读，绝不回写）。\n" +
			"不带子命令运行等价于 agyquota quota。",
		Version:       Version,
		SilenceErrors: true,
		SilenceUsage:  true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return quotaRun(cmd)
		},
	}
	root.PersistentFlags().Bool("json", false, "以 JSON 信封输出（供脚本/程序消费）")
	root.PersistentFlags().String("token-file", "", "凭据文件路径（缺省用 ~/.gemini/antigravity-acp/acp_token.json）")
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

// schemaCmd 导出与 tools/list 同源的 MCP 工具定义（输出即 JSON，无业务调用）。
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
