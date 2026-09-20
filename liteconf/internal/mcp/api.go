// Package mcp 实现 liteconf 的 MCP 适配器：
// 以官方 go-sdk 提供 stdio MCP server 入口，数据面经 HTTP 调用
// 运行中 server 的 /api/*（与 Web 控制台、curlie 同一地位），
// 不复制 server 业务逻辑，client SDK（../client）定位不受影响。
package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// 业务错误 code：server 端包络 code 原样透传，客户端侧新增三条
const (
	// CodeUnreachable server 地址不可达（连接失败、超时）
	CodeUnreachable = "server_unreachable"
	// CodeInternal 客户端侧意外错误（响应非法、序列化失败等）
	CodeInternal = "internal"
	// CodePathNotFound config.get 的点路径未命中（server 端无此概念）
	CodePathNotFound = "path_not_found"
)

// maxRespSize 限制响应体大小，防滥用（对齐 client/client.go）
const maxRespSize = 8 << 20

// apiTimeout 常规读写请求超时（无 watch 长请求，15s 足够）
const apiTimeout = 15 * time.Second

// apiError 稳定业务错误：server 包络 code 原样透传
// （not_found / invalid_name / invalid_json / internal），
// 客户端侧新增 CodeUnreachable / CodeInternal。
type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Error 实现 error 接口
func (e *apiError) Error() string { return e.Message }

// asAPIError 把任意错误归一为 *apiError：已是 apiError 则透传，
// 意外错误兜底为 CodeInternal。
func asAPIError(err error) *apiError {
	var ae *apiError
	if errors.As(err, &ae) {
		return ae
	}
	return &apiError{Code: CodeInternal, Message: err.Error()}
}

// apiClient 调用运行中 server 的 HTTP API
type apiClient struct {
	base string
	hc   *http.Client
}

// newAPIClient 创建 API 客户端；serverURL 形如 http://127.0.0.1:8646
func newAPIClient(serverURL string) *apiClient {
	return &apiClient{
		base: strings.TrimRight(serverURL, "/"),
		hc:   &http.Client{Timeout: apiTimeout},
	}
}

// apiEnvelope server 统一响应包络 {"code","message","data"}
type apiEnvelope struct {
	Code    string          `json:"code"`
	Message string          `json:"message,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// discoveryData GET /api/discovery 的返回形状
type discoveryData struct {
	Apps []discoveryApp `json:"apps"`
}

// discoveryApp 发现接口的单应用条目
type discoveryApp struct {
	App  string         `json:"app"`
	Envs []discoveryEnv `json:"envs"`
}

// discoveryEnv 发现接口的单环境条目
type discoveryEnv struct {
	Env     string `json:"env"`
	Version uint64 `json:"version"`
}

// call 执行一次 API 调用并返回包络 data：
// 网络失败 → CodeUnreachable；包络 code != ok 或非 200 → 透传 code；
// 响应形状异常 → CodeInternal。
func (c *apiClient) call(ctx context.Context, method, path string, body any) (json.RawMessage, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, &apiError{Code: CodeInternal, Message: "marshal request body: " + err.Error()}
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rd)
	if err != nil {
		return nil, &apiError{Code: CodeInternal, Message: "build request: " + err.Error()}
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, &apiError{Code: CodeUnreachable, Message: fmt.Sprintf("server %s unreachable: %v", c.base, err)}
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxRespSize+1))
	if err != nil {
		return nil, &apiError{Code: CodeInternal, Message: fmt.Sprintf("read response (status %d): %v", resp.StatusCode, err)}
	}
	if len(raw) > maxRespSize {
		return nil, &apiError{Code: CodeInternal, Message: fmt.Sprintf("response too large (>%d bytes)", maxRespSize)}
	}

	var env apiEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, &apiError{Code: CodeInternal, Message: fmt.Sprintf("response is not valid json (status %d)", resp.StatusCode)}
	}
	if resp.StatusCode != http.StatusOK || env.Code != "ok" {
		code := env.Code
		if code == "" {
			code = CodeInternal
		}
		msg := env.Message
		if msg == "" {
			msg = http.StatusText(resp.StatusCode)
		}
		return nil, &apiError{Code: code, Message: msg}
	}
	return env.Data, nil
}

// configPath 拼接 /api/{app}/{env}；防御性 PathEscape
func configPath(app, env string) string {
	return "/api/" + url.PathEscape(app) + "/" + url.PathEscape(env)
}

// discovery 列出全部应用/环境/版本
func (c *apiClient) discovery(ctx context.Context) (discoveryData, error) {
	data, err := c.call(ctx, http.MethodGet, "/api/discovery", nil)
	if err != nil {
		return discoveryData{}, err
	}
	var d discoveryData
	if err := json.Unmarshal(data, &d); err != nil {
		return discoveryData{}, &apiError{Code: CodeInternal, Message: "decode discovery data: " + err.Error()}
	}
	if d.Apps == nil {
		d.Apps = []discoveryApp{}
	}
	return d, nil
}

// get 读取配置内容与版本
func (c *apiClient) get(ctx context.Context, app, env string) (json.RawMessage, uint64, error) {
	data, err := c.call(ctx, http.MethodGet, configPath(app, env), nil)
	if err != nil {
		return nil, 0, err
	}
	var d struct {
		Content json.RawMessage `json:"content"`
		Version uint64          `json:"version"`
	}
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, 0, &apiError{Code: CodeInternal, Message: "decode config data: " + err.Error()}
	}
	return d.Content, d.Version, nil
}

// put 整体覆盖写入配置，返回 server 端生成的新版本号
func (c *apiClient) put(ctx context.Context, app, env string, content map[string]any) (uint64, error) {
	data, err := c.call(ctx, http.MethodPut, configPath(app, env), content)
	if err != nil {
		return 0, err
	}
	var d struct {
		Version uint64 `json:"version"`
	}
	if err := json.Unmarshal(data, &d); err != nil {
		return 0, &apiError{Code: CodeInternal, Message: "decode put data: " + err.Error()}
	}
	return d.Version, nil
}
