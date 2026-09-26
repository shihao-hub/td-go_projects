// Package api 是 agyquota 的共享契约包：CLI、MCP 桥、daemon 与 client
// 共用同一份数据源枚举、稳定错误码、DTO、HTTP 端点与 JSON 包络定义，
// 避免类型、帮助与协议各自漂移。
package api

import (
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// 数据源枚举（对外契约）。
const (
	SourceAgy = "agy"
	SourceZed = "zed"
)

// 稳定业务错误码（公开契约）。
const (
	ErrSourceRequired = "source_required"
	ErrAgyNotFound    = "agy_not_found"
	ErrAgyExecute     = "agy_execute_failed"
	ErrZedExecute     = "zed_execute_failed"
	ErrParse          = "response_parse_failed"
	ErrBadArgs        = "bad_args"

	// daemon 连接类错误码（v2 daemon 架构新增）。
	ErrDaemonUnreachable = "daemon_unreachable"
	ErrDaemonStartFailed = "daemon_start_failed"
	ErrBuildMismatch     = "build_mismatch"

	// ErrInternal 是未知错误的兜底码。
	ErrInternal = "internal"
)

// HTTP 端点（daemon 与 client 共用）。
const (
	EndpointPing     = "/v1/ping"
	EndpointQuotaGet = "/v1/quota/get"
	EndpointQuotaRaw = "/v1/quota/raw"
	EndpointStop     = "/v1/stop"
)

// HeaderBuild 是 buildID 握手请求头（仅 /v1/quota/* 校验；ping/stop 豁免）。
const HeaderBuild = "X-Agyquota-Build"

// Error 公共业务错误：稳定 code + 可公开 message + 可选建议。
// 不携带入口语义（CLI 退出码 / MCP isError），由入口自行映射。
type Error struct {
	Code        string   `json:"code"`
	Message     string   `json:"message"`
	Suggestions []string `json:"suggestions,omitempty"`
}

func (e *Error) Error() string { return e.Message }

// Errorf 构造业务错误（不带建议）。
func Errorf(code, format string, a ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, a...)}
}

// WithSuggestion 追加一条建议并返回自身，便于链式构造。
func (e *Error) WithSuggestion(s string) *Error {
	e.Suggestions = append(e.Suggestions, s)
	return e
}

// Window 单个配额窗口（如 5 小时窗口 / 每周窗口）。
type Window struct {
	Window            string   `json:"window"`                      // 归一化标识：five_hour / weekly / daily / "quota"
	Label             string   `json:"label"`                       // 人读短标签：5h / weekly / …
	Percent           float64  `json:"percent"`                     // 剩余百分比 0..100（保留 1 位小数）
	RemainingFraction *float64 `json:"remainingFraction,omitempty"` // 接口原始小数
	ResetAt           string   `json:"resetAt,omitempty"`           // 重置时间（尽量 RFC3339）
	ResetsIn          string   `json:"resetsIn,omitempty"`          // 人读剩余："6天21小时后刷新"
}

// ModelQuota 单模型的配额窗口集合。
type ModelQuota struct {
	Name    string   `json:"name"`
	Windows []Window `json:"windows"`
}

// Bucket 模型桶。
type Bucket struct {
	Name   string       `json:"name"`
	Models []ModelQuota `json:"models"`
}

// Snapshot 一次配额查询结果。
type Snapshot struct {
	Source         string    `json:"source"`
	Account        string    `json:"account,omitempty"`        // 账号邮箱
	TokenExpiresAt string    `json:"tokenExpiresAt,omitempty"` // 凭据到期时间（仅 Zed 模式）
	TokenExpiresIn string    `json:"tokenExpiresIn,omitempty"` // 剩余有效期（仅 Zed 模式）
	FetchedAt      time.Time `json:"fetchedAt"`
	Buckets        []Bucket  `json:"buckets"`
}

// QuotaRequest 配额查询请求体（source=agy|zed，tokenFile 仅 zed 生效）。
type QuotaRequest struct {
	Source    string `json:"source,omitempty"`
	TokenFile string `json:"tokenFile,omitempty"`
}

// PingResponse 探测响应：daemon 身份与 buildID 握手信息。
type PingResponse struct {
	Name      string    `json:"name"`
	Version   string    `json:"version"`
	BuildID   string    `json:"buildID"`
	PID       int       `json:"pid"`
	StartedAt time.Time `json:"startedAt"`
}

// RawResponse 原始配额响应（daemon 以 data.raw 承载 --raw 结果）。
type RawResponse struct {
	Raw json.RawMessage `json:"raw"`
}

// StopResponse stop 结果。
type StopResponse struct {
	Stopped bool `json:"stopped"`
}

// Envelope 是 v1 JSON 包络：成功体无 error 键、失败体无 data 键。
type Envelope struct {
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data,omitempty"`
	Error *Error          `json:"error,omitempty"`
}

// WriteEnvelope 以 2 空格缩进写入包络并补一个换行（CLI 与 daemon 共用）。
func WriteEnvelope(w io.Writer, env Envelope) error {
	b, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(b))
	return err
}

// WriteOK 以成功包络写入 data。
func WriteOK(w io.Writer, data any) error {
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return WriteEnvelope(w, Envelope{OK: true, Data: b})
}

// WriteErr 以失败包络写入错误。
func WriteErr(w io.Writer, e *Error) error {
	return WriteEnvelope(w, Envelope{OK: false, Error: e})
}

// MarshalEnvelope 返回单行紧凑包络（SSE result 帧用）。
func MarshalEnvelope(env Envelope) ([]byte, error) {
	return json.Marshal(env)
}

// ReadEnvelope 从 r 读取并解析一个包络。
func ReadEnvelope(r io.Reader) (*Envelope, error) {
	var env Envelope
	if err := json.NewDecoder(r).Decode(&env); err != nil {
		return nil, err
	}
	return &env, nil
}
