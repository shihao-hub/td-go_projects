package daemon

import (
	"fmt"
	"time"

	"agyquota/internal/api"

	"github.com/spf13/cobra"
)

// NewServeCmd 构造 `agyquota serve` 命令（由 cmd/agyquota 挂载到根命令）。
// 放在 daemon 包内，避免 internal/cli 依赖 service（依赖方向硬约束）。
func NewServeCmd() *cobra.Command {
	var (
		bind        string
		port        int
		idleTimeout time.Duration
	)
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "以 daemon 方式运行（回环 HTTP API，供 CLI/MCP 调用）",
		Long: "在前台运行 agyquota daemon：在回环地址监听 HTTP+JSON API，写出地址文件，\n" +
			"Ctrl+C 优雅退出。前台 serve 不设空闲退出；自动拉起的 daemon 由客户端注入\n" +
			"--idle-timeout 30m 控制。",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if port < 0 || port > 65535 {
				return fmt.Errorf("--port 越界: %d（允许 0–65535）", port)
			}
			if idleTimeout < 0 {
				return fmt.Errorf("--idle-timeout 不能为负数: %s", idleTimeout)
			}
			err := Serve(cmd.Context(), Config{Bind: bind, Port: port, IdleTimeout: idleTimeout})
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "错误: %v\n", err)
				return &api.Error{Code: api.ErrInternal, Message: err.Error()}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&bind, "bind", "127.0.0.1", "监听地址（仅本机使用，默认回环）")
	cmd.Flags().IntVar(&port, "port", 17625, "监听端口（0 表示随机可用端口）")
	cmd.Flags().DurationVar(&idleTimeout, "idle-timeout", 0, "空闲自动退出时长（如 30m；0 表示不退出）")
	return cmd
}
