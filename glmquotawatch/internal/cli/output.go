package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"glmquotawatch/internal/service"

	"github.com/spf13/cobra"
)

// ---- 输出适配 ----
// 人读模式：数据到 stdout、错误到 stderr；--json 模式：成功与错误统一以
// JSON 信封输出到 stdout（供脚本/程序消费）。

// envelope JSON 信封。Data 与 Error 互斥。
type envelope struct {
	OK    bool    `json:"ok"`
	Data  any     `json:"data,omitempty"`
	Error *errObj `json:"error,omitempty"`
}

type errObj struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// codedError 携带业务错误的失败信号（退出码 1），由 Run 统一识别。
// cobra 产生的 flag/参数错误不走此类型，Run 兜底为退出码 2。
type codedError struct{ err error }

func (e *codedError) Error() string { return e.err.Error() }
func (e *codedError) Unwrap() error { return e.err }

func jsonMode(cmd *cobra.Command) bool {
	v, err := cmd.Flags().GetBool("json")
	return err == nil && v
}

func emitJSON(w io.Writer, data any) {
	b, err := json.MarshalIndent(envelope{OK: true, Data: data}, "", "  ")
	if err != nil {
		return
	}
	fmt.Fprintln(w, string(b))
}

func emitErrJSON(w io.Writer, se *service.Error) {
	b, err := json.MarshalIndent(envelope{OK: false, Error: &errObj{Code: se.Code, Message: se.Message}}, "", "  ")
	if err != nil {
		return
	}
	fmt.Fprintln(w, string(b))
}

// outOK 输出成功结果（人读或信封），始终返回 nil。
func outOK(cmd *cobra.Command, data any) error {
	if jsonMode(cmd) {
		emitJSON(os.Stdout, data)
		return nil
	}
	printHuman(os.Stdout, data)
	return nil
}

// outErr 输出错误并返回 codedError（退出码 1）。
func outErr(cmd *cobra.Command, err error) error {
	se := asSvcErr(err)
	if jsonMode(cmd) {
		emitErrJSON(os.Stdout, se)
	} else {
		fmt.Fprintf(os.Stderr, "错误: %s\n", se.Message)
	}
	return &codedError{err: se}
}

func asSvcErr(err error) *service.Error {
	var se *service.Error
	if errors.As(err, &se) {
		return se
	}
	return &service.Error{Code: "internal", Message: err.Error()}
}

// printHuman 人读输出。新增视图类型时在此补充分支。
func printHuman(w io.Writer, data any) {
	switch v := data.(type) {
	case service.ConfigView:
		token := "（未配置）"
		if v.HasToken {
			token = v.Token
		}
		fmt.Fprintf(w, "数据目录:   %s\n", v.Dir)
		fmt.Fprintf(w, "token:      %s\n", token)
		fmt.Fprintf(w, "interval:   %s\n", v.Interval)
		fmt.Fprintf(w, "thresholds: %s\n", joinInts(v.Thresholds))
		fmt.Fprintf(w, "hysteresis: %d\n", v.Hysteresis)
		fmt.Fprintf(w, "silent:     %v\n", v.Silent)
	case service.StatusView:
		fmt.Fprintf(w, "GLM 用量（套餐 %s，采样于 %s）\n", v.Level, v.SampledAt)
		for _, win := range v.Windows {
			notified := "无"
			if len(win.Notified) > 0 {
				notified = joinInts(win.Notified)
			}
			reset := ""
			if win.ResetIn != "" {
				reset = fmt.Sprintf("，距下次刷新还有 %s", win.ResetIn)
			}
			fmt.Fprintf(w, "  %-12s %3d%%    已通知档位: %s%s\n", win.Label, win.Percentage, notified, reset)
		}
	default:
		fmt.Fprintf(w, "%+v\n", data)
	}
}

func joinInts(xs []int) string {
	parts := make([]string, 0, len(xs))
	for _, x := range xs {
		parts = append(parts, fmt.Sprintf("%d", x))
	}
	return strings.Join(parts, ", ")
}
