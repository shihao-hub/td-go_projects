package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"plansql/internal/buildinfo"
	"plansql/internal/core"
	"plansql/internal/daemon"
	"time"
)

// Client 负责向 plansql daemon 发起 HTTP 请求的薄客户端
type Client struct {
	baseURL    string
	explicit   bool
	httpClient *http.Client
}

// NewClient 按标准 v2 优先级查找 Daemon：
// 1. 显式指定的 host (--host)
// 2. PLANSQL_HOST 环境变量
// 3. %APPDATA%\language_projects\plansql\plansql.addr 地址文件
// 4. 兜底 127.0.0.1:18090
func NewClient(explicitHost string) (*Client, error) {
	host := explicitHost
	explicit := false
	if host != "" {
		explicit = true
	}
	if host == "" {
		if env := os.Getenv("PLANSQL_HOST"); env != "" {
			host = env
			explicit = true
		}
	}
	if host == "" {
		if addr, err := daemon.ReadAddressFile(); err == nil && addr != "" {
			host = addr
		}
	}
	if host == "" {
		host = "127.0.0.1:18090"
	}

	return &Client{
		baseURL:    "http://" + host,
		explicit:   explicit,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}, nil
}

// EnsureDaemonRunning 严格遵循标准 v2 第 3.2 节：
// - 显式指定地址时：连不上直接报错，绝不在本地拉一个；
// - 开发构建（非干净 git tag，如 -dirty / dev）：连不上直接报错，提示用户使用开发双终端流先运行 plansql serve；
// - 生产构建（干净 release tag）：连不上时自动 detach 拉起。
func EnsureDaemonRunning(client *Client, rootDir, sqlFile string) error {
	// 1. 先探活
	if err := client.Ping(); err == nil {
		return nil
	}

	// 2. 显式指定地址时绝不自动拉起
	if client.explicit {
		return fmt.Errorf("daemon %s 无法连接（显式指定地址时不自动拉起）", client.baseURL)
	}

	// 3. 开发构建绝不自动拉起，强制要求开发双终端
	if !buildinfo.IsProduction() {
		return fmt.Errorf("plansql daemon 未运行（开发构建不自动拉起，build: %s）。请先在另一终端运行 `plansql serve`", buildinfo.BuildID)
	}

	// 4. 仅限生产构建自动拉起
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}

	fmt.Fprintf(os.Stderr, "[cli] daemon not running, auto-spawning detached server (production build)...\n")
	if err := daemon.SpawnDaemonDetached(exePath, rootDir, sqlFile); err != nil {
		return fmt.Errorf("spawn daemon: %w", err)
	}

	// 轮询等待拉起就绪（最多 3 秒）
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(150 * time.Millisecond)
		// 重新加载地址文件
		if addr, err := daemon.ReadAddressFile(); err == nil && addr != "" {
			client.baseURL = "http://" + addr
		}
		if err := client.Ping(); err == nil {
			return nil
		}
	}

	return fmt.Errorf("daemon failed to start within timeout")
}

// Ping 检查健康
func (c *Client) Ping() error {
	resp, err := c.httpClient.Get(c.baseURL + "/api/v1/items")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status code %d", resp.StatusCode)
	}
	return nil
}

func (c *Client) doRequest(method, path string, inBody any, outData any) error {
	var bodyReader io.Reader
	if inBody != nil {
		data, err := json.Marshal(inBody)
		if err != nil {
			return err
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, c.baseURL+path, bodyReader)
	if err != nil {
		return err
	}
	if inBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var apiResp daemon.APIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	if !apiResp.OK {
		return fmt.Errorf("api error: %s", apiResp.Error)
	}

	if outData != nil && apiResp.Data != nil {
		raw, err := json.Marshal(apiResp.Data)
		if err != nil {
			return err
		}
		return json.Unmarshal(raw, outData)
	}

	return nil
}

// GetItems 获取所有条目
func (c *Client) GetItems() ([]core.Item, error) {
	var items []core.Item
	err := c.doRequest(http.MethodGet, "/api/v1/items", nil, &items)
	return items, err
}

// CheckSQL 校验 SQL
func (c *Client) CheckSQL() (*core.CheckResult, error) {
	var res core.CheckResult
	err := c.doRequest(http.MethodPost, "/api/v1/check", nil, &res)
	return &res, err
}

// ScanDocs 扫描对齐
func (c *Client) ScanDocs() (*core.ScanReport, error) {
	var report core.ScanReport
	err := c.doRequest(http.MethodGet, "/api/v1/scan", nil, &report)
	return &report, err
}

// AppendMutation 追加状态
func (c *Client) AppendMutation(itemType, relPath, statusJSON string) (string, error) {
	req := daemon.MutationRequest{
		Type:   itemType,
		Path:   relPath,
		Status: statusJSON,
	}
	var res map[string]any
	err := c.doRequest(http.MethodPost, "/api/v1/append", req, &res)
	if err != nil {
		return "", err
	}
	if sqlStr, ok := res["sql"].(string); ok {
		return sqlStr, nil
	}
	return "", nil
}
