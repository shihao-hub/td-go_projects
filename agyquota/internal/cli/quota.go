package cli

import (
	"fmt"
	"time"

	"agyquota/internal/api"
	"agyquota/internal/client"

	"github.com/spf13/cobra"
)

// quotaCmd 显式 quota 子命令；根命令默认行为与其一致。
func quotaCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "quota",
		Short: "查询 Antigravity 模型配额（需指定 --agy 或 --zed）",
		Long: "查询各模型桶（Gemini / Claude and GPT）的配额剩余百分比与重置时间。\n\n" +
			"必须显式指定查询数据源：\n" +
			"  --agy : 调用本地官方 Antigravity CLI (agy)，查询终端/桌面端配额，执行完强制回收进程；\n" +
			"  --zed : 读取 Zed (antigravity-acp) 凭据直连 Google 官方接口，带 50 分钟本地缓存与官方 UA 伪装。",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return quotaRun(cmd)
		},
	}
}

// quotaRun 是 quota 的执行体：解析参数 → daemon HTTP 调用 → 渲染。
func quotaRun(cmd *cobra.Command) error {
	raw, _ := cmd.Flags().GetBool("raw")
	tokenFile, _ := cmd.Flags().GetString("token-file")
	useAgy, _ := cmd.Flags().GetBool("agy")
	useZed, _ := cmd.Flags().GetBool("zed")

	if !useAgy && !useZed {
		return outErr(cmd, &api.Error{
			Code:    api.ErrSourceRequired,
			Message: "未指定查询数据源",
			Suggestions: []string{
				"使用 agyquota --agy 查询终端与桌面 GUI 账号配额",
				"使用 agyquota --zed 查询 Zed (antigravity-acp) 账号配额",
			},
		})
	}

	if useAgy && useZed {
		return outErr(cmd, &api.Error{
			Code:    api.ErrBadArgs,
			Message: "--agy 与 --zed 为互斥选项，请每次指定一个数据源",
		})
	}

	source := api.SourceAgy
	if useZed {
		source = api.SourceZed
	}

	host, _ := cmd.Flags().GetString("host")
	cl := client.New(client.Config{Host: host})
	ctx, cancel := contextTimeout(cmd, 2*time.Minute)
	defer cancel()

	// 进度与退避提示走 stderr（人读/JSON 模式均合法：stderr 是诊断流）
	progress := func(msg string) {
		fmt.Fprintf(cmd.ErrOrStderr(), "[agyquota] %s\n", msg)
	}

	if _, _, err := cl.Resolve(); err != nil {
		return outErr(cmd, err)
	}
	if err := cl.EnsureDaemon(ctx); err != nil {
		return outErr(cmd, err)
	}

	req := api.QuotaRequest{Source: source, TokenFile: tokenFile}
	if raw {
		rawJSON, err := cl.GetRaw(ctx, req, progress)
		if err != nil {
			return outErr(cmd, err)
		}
		return outRaw(cmd, rawJSON)
	}
	snap, err := cl.GetQuota(ctx, req, progress)
	if err != nil {
		return outErr(cmd, err)
	}
	return outQuota(cmd, snap)
}
