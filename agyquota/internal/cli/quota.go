package cli

import (
	"fmt"
	"time"

	"agyquota/internal/service"

	"github.com/spf13/cobra"
)

// quotaCmd 显式 quota 子命令；根命令默认行为与其一致。
func quotaCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "quota",
		Short: "查询 Antigravity 模型配额（与不带子命令运行等价）",
		Long: "查询各模型桶（Gemini / Claude and GPT）的配额剩余百分比与重置时间。\n" +
			"接口限流较严：遇 429 自动退避重试（15s/45s/90s），请耐心等待；\n" +
			"Antigravity IDE 正在运行时会竞争限额，建议关闭后查询。",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return quotaRun(cmd)
		},
	}
}

// quotaRun 是 quota 的执行体：调用公共 service 并渲染。
// 接口带 429 退避重试，整体预算放宽到 4 分钟。
func quotaRun(cmd *cobra.Command) error {
	raw, _ := cmd.Flags().GetBool("raw")
	tokenFile, _ := cmd.Flags().GetString("token-file")

	svc := service.New()
	// 进度与退避提示走 stderr（人读/JSON 模式均合法：stderr 是诊断流）
	svc.Progress = func(f string, a ...any) {
		fmt.Fprintf(cmd.ErrOrStderr(), "[agyquota] "+f+"\n", a...)
	}
	ctx, cancel := contextTimeout(cmd, 4*time.Minute)
	defer cancel()
	opt := service.Options{TokenFile: tokenFile}

	if raw {
		rawJSON, err := svc.FetchRaw(ctx, opt)
		if err != nil {
			return outErr(cmd, err)
		}
		return outRaw(cmd, rawJSON)
	}
	snap, err := svc.GetQuota(ctx, opt)
	if err != nil {
		return outErr(cmd, err)
	}
	return outQuota(cmd, snap)
}
