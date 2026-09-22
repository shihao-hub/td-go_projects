package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"agyquota/internal/service"

	"github.com/spf13/cobra"
)

// contextTimeout 从命令上下文派生带超时的 context。
func contextTimeout(cmd *cobra.Command, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(cmd.Context(), d)
}

// jsonMode 读取 --json flag（不可用时按人读处理）。
func jsonMode(cmd *cobra.Command) bool {
	v, err := cmd.Flags().GetBool("json")
	return err == nil && v
}

// emitOK 输出成功 JSON 信封：{"ok":true,"data":...}。
func emitOK(w io.Writer, data any) {
	b, _ := json.MarshalIndent(map[string]any{"ok": true, "data": data}, "", "  ")
	fmt.Fprintln(w, string(b))
}

// emitErrJSON 输出失败 JSON 信封：{"ok":false,"error":{code,message,suggestions}}。
func emitErrJSON(w io.Writer, e *service.Error) {
	errObj := map[string]any{"code": e.Code, "message": e.Message}
	if len(e.Suggestions) > 0 {
		errObj["suggestions"] = e.Suggestions
	}
	b, _ := json.MarshalIndent(map[string]any{"ok": false, "error": errObj}, "", "  ")
	fmt.Fprintln(w, string(b))
}

// outErr 统一错误出口：人读写 stderr 并附建议；--json 写 stdout 信封。
// 返回 *service.Error 供 Run 映射退出码 1。
func outErr(cmd *cobra.Command, err error) error {
	var berr *service.Error
	if !errors.As(err, &berr) {
		berr = &service.Error{Code: "internal", Message: err.Error()}
	}
	if jsonMode(cmd) {
		emitErrJSON(cmd.OutOrStdout(), berr)
	} else {
		fmt.Fprintf(cmd.ErrOrStderr(), "错误[%s]: %s\n", berr.Code, berr.Message)
		for _, s := range berr.Suggestions {
			fmt.Fprintf(cmd.ErrOrStderr(), "  建议: %s\n", s)
		}
	}
	return berr
}

// outQuota 人读/JSON 输出配额快照。
func outQuota(cmd *cobra.Command, snap *service.Snapshot) error {
	if jsonMode(cmd) {
		emitOK(cmd.OutOrStdout(), snap)
		return nil
	}
	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "Antigravity 模型配额（查询于 %s）\n", snap.FetchedAt.Format("2006-01-02 15:04:05"))
	if len(snap.Buckets) == 0 {
		fmt.Fprintln(w, "  未解析到配额数据；接口响应结构可能已变化，可用 --raw 查看原始响应。")
		return nil
	}
	for i, b := range snap.Buckets {
		if i > 0 {
			fmt.Fprintln(w)
		}
		fmt.Fprintln(w, b.Name)
		for _, m := range b.Models {
			for _, win := range m.Windows {
				fmt.Fprintf(w, "  %-28s %-8s %5.0f%%  %s\n", m.Name, win.Label, win.Percent, win.ResetsIn)
			}
		}
	}
	return nil
}

// outRaw 输出接口原始响应：人读为缩进 JSON；--json 时装进 data.raw 信封。
func outRaw(cmd *cobra.Command, raw json.RawMessage) error {
	if jsonMode(cmd) {
		emitOK(cmd.OutOrStdout(), map[string]any{"raw": raw})
		return nil
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, raw, "", "  "); err != nil {
		fmt.Fprintln(cmd.OutOrStdout(), string(raw))
		return nil
	}
	fmt.Fprintln(cmd.OutOrStdout(), pretty.String())
	return nil
}
