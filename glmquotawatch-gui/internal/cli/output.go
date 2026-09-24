package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"glmquotawatch-gui/internal/service"

	"github.com/spf13/cobra"
)

// ---- 输出适配（恒 JSON）----
// 本 CLI 面向机器消费：成功与失败统一以 JSON 信封输出到 stdout；
// stderr 只放诊断日志。人读入口是 GUI。
//
// 信封格式（与《CLI 工具开发标准》§4.1 一致）：
//
//	{"ok": true, "data": <结果视图>}
//	{"ok": false, "error": {"code": "<业务错误码>", "message": "<说明>"}}

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

func emitJSON(w io.Writer, data any) {
	b, err := marshalIndent(envelope{OK: true, Data: data})
	if err != nil {
		return
	}
	fmt.Fprintln(w, string(b))
}

func emitErrJSON(w io.Writer, se *service.Error) {
	b, err := marshalIndent(envelope{OK: false, Error: &errObj{Code: se.Code, Message: se.Message}})
	if err != nil {
		return
	}
	fmt.Fprintln(w, string(b))
}

func marshalIndent(v any) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}

// outOK 输出成功信封，始终返回 nil。
func outOK(_ *cobra.Command, data any) error {
	emitJSON(os.Stdout, data)
	return nil
}

// outErr 输出失败信封并返回 codedError（退出码 1）。
func outErr(_ *cobra.Command, err error) error {
	se := asSvcErr(err)
	emitErrJSON(os.Stdout, se)
	return &codedError{err: se}
}

func asSvcErr(err error) *service.Error {
	var se *service.Error
	if errors.As(err, &se) {
		return se
	}
	return &service.Error{Code: "internal", Message: err.Error()}
}
