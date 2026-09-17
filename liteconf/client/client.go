// Package client 提供 liteconf 配置中心 Go SDK：
// 初始化同步拉取、无锁缓存读取（dot path / 完整快照 / struct 反序列化）、
// OnChange 变更订阅回调与断线指数退避自动重连。
//
// 用法：
//
//	c, err := client.New(client.Config{ServerURL: "http://127.0.0.1:8646", App: "app1", Env: "dev"})
//	if err != nil { return err }
//	defer c.Close()
//	c.OnChange(func(old, new map[string]any) { ... })
//	v, ok := c.Get("db.host")
package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// 默认请求超时：须大于 server 端 30s 长轮询超时
const defaultRequestTimeout = 35 * time.Second

// Config SDK 初始化配置
type Config struct {
	// ServerURL liteconf server 地址，如 http://127.0.0.1:8646
	ServerURL string
	// App 应用名
	App string
	// Env 环境名
	Env string
	// RequestTimeout HTTP 请求超时，零值取默认 35s
	RequestTimeout time.Duration
}

// Client liteconf SDK 客户端
type Client struct {
	cfg      Config
	http     *http.Client
	cache    cache
	cb       callbacks
	pollCtx  context.Context
	pollStop context.CancelFunc
}

// apiResponse 统一响应包络的解析结构
type apiResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Data    struct {
		Content json.RawMessage `json:"content"`
		Version uint64          `json:"version"`
	} `json:"data"`
}

// New 初始化 SDK：同步拉取一次配置并缓存。
// 失败返回明确错误（含重试建议），不返回空配置伪装成功。
func New(cfg Config) (*Client, error) {
	if cfg.ServerURL == "" || cfg.App == "" || cfg.Env == "" {
		return nil, errors.New("liteconf: ServerURL/App/Env must not be empty")
	}
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = defaultRequestTimeout
	}
	c := &Client{
		cfg:  cfg,
		http: &http.Client{Timeout: cfg.RequestTimeout},
	}
	c.pollCtx, c.pollStop = context.WithCancel(context.Background())

	// 初始化同步拉取
	content, version, err := c.fetch(context.Background())
	if err != nil {
		c.pollStop()
		return nil, fmt.Errorf("liteconf: init fetch %s/%s from %s failed: %w（请检查 server 地址与网络后重试）",
			cfg.App, cfg.Env, cfg.ServerURL, err)
	}
	c.cache.swap(content, version)

	go c.run(c.pollCtx)
	return c, nil
}

// Get 按键读取，path 支持 "db.host" 点路径；bool 为存在性标识而非静默零值。
// path 为空返回整份配置。
func (c *Client) Get(path string) (json.RawMessage, bool) {
	st := c.cache.load()
	if st == nil {
		return nil, false
	}
	v, ok := lookup(st.content, path)
	if !ok {
		return nil, false
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, false
	}
	return raw, true
}

// Snapshot 返回完整配置对象的浅拷贝
func (c *Client) Snapshot() map[string]any {
	st := c.cache.load()
	if st == nil {
		return map[string]any{}
	}
	return snapshotOf(st.content)
}

// Unmarshal 将当前缓存配置（或 path 指向的子对象）解码到调用方 struct；
// path 为空则解码整份配置
func (c *Client) Unmarshal(path string, v any) error {
	raw, ok := c.Get(path)
	if !ok {
		return fmt.Errorf("liteconf: path %q not found in config %s/%s", path, c.cfg.App, c.cfg.Env)
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("liteconf: unmarshal path %q: %w", path, err)
	}
	return nil
}

// Version 返回本地已知版本号
func (c *Client) Version() uint64 {
	return c.cache.version()
}

// Close 停止后台长轮询，幂等可多次调用
func (c *Client) Close() {
	c.pollStop()
}

// fetch 从 server 拉取当前配置内容与版本
func (c *Client) fetch(ctx context.Context) (map[string]any, uint64, error) {
	url := fmt.Sprintf("%s/api/%s/%s", strings.TrimRight(c.cfg.ServerURL, "/"), c.cfg.App, c.cfg.Env)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxRespSize))
	if err != nil {
		return nil, 0, err
	}
	var ar apiResponse
	if err := json.Unmarshal(body, &ar); err != nil {
		return nil, 0, fmt.Errorf("decode response (status %d): %w", resp.StatusCode, err)
	}
	if resp.StatusCode != http.StatusOK || ar.Code != "ok" {
		return nil, 0, fmt.Errorf("server error (status %d): code=%s message=%s", resp.StatusCode, ar.Code, ar.Message)
	}
	var content map[string]any
	if err := json.Unmarshal(ar.Data.Content, &content); err != nil {
		return nil, 0, fmt.Errorf("decode config content: %w", err)
	}
	return content, ar.Data.Version, nil
}

// maxRespSize 限制响应体大小，防滥用
const maxRespSize = 8 << 20
