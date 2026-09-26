// Package client 是 daemon 的共享 HTTP 客户端：地址发现、生产自动拉起、
// buildID 握手、SSE 进度解析与错误映射；CLI 与 MCP 桥都基于它。
// 本包不依赖 internal/service（薄壳依赖方向硬约束）。
package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"agyquota/internal/api"
	"agyquota/internal/appdata"
	"agyquota/internal/buildinfo"
)

const (
	// DefaultPort 是 daemon 默认监听端口。
	DefaultPort = 17625
	// DefaultAddr 是最终兜底的地址。
	DefaultAddr = "127.0.0.1:17625"
	// spawnIdleTimeout 是自动拉起的 daemon 的空闲退出时长。
	spawnIdleTimeout = "30m"
	// pingTimeout 是探测/握手超时。
	pingTimeout = 500 * time.Millisecond
	// startWait 是自动拉起后就绪等待上限。
	startWait = 10 * time.Second
	// startPollInterval 是就绪轮询间隔。
	startPollInterval = 100 * time.Millisecond
)

// Config 是客户端配置。
type Config struct {
	Host string // 显式目标地址 host[:port]（--host）；空时按发现优先级解析
}

// Client 是 daemon 的 HTTP 客户端；用 New 构造。
type Client struct {
	cfg      Config
	hc       *http.Client
	addr     string
	explicit bool
	resolved bool
}

// New 创建客户端。
func New(cfg Config) *Client {
	return &Client{cfg: cfg, hc: &http.Client{}}
}

// NormalizeHost 校验并归一化 host[:port]：缺省端口补 17625。
func NormalizeHost(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", errors.New("地址为空")
	}
	if strings.Contains(s, "://") {
		return "", errors.New("仅接受 host[:port] 形式（不含协议前缀）")
	}
	host, port, err := net.SplitHostPort(s)
	if err != nil {
		var addrErr *net.AddrError
		if errors.As(err, &addrErr) && strings.Contains(addrErr.Err, "missing port") {
			host = strings.Trim(s, "[]")
			port = ""
		} else {
			return "", fmt.Errorf("地址格式无效: %q", s)
		}
	}
	if host == "" {
		return "", errors.New("主机名称为空")
	}
	if port == "" {
		port = strconv.Itoa(DefaultPort)
	}
	p, err := strconv.Atoi(port)
	if err != nil || p < 1 || p > 65535 {
		return "", fmt.Errorf("端口无效: %q", port)
	}
	return net.JoinHostPort(host, port), nil
}

// Resolve 按固定优先级解析 daemon 地址：
// --host → AGYQUOTA_HOST → 地址文件 → 内置默认端口。
// explicit 表示地址由 --host / 环境变量显式指定（显式地址绝不自动拉起）。
func (c *Client) Resolve() (string, bool, error) {
	if c.cfg.Host != "" {
		addr, err := NormalizeHost(c.cfg.Host)
		if err != nil {
			return "", true, api.Errorf(api.ErrBadArgs, "--host 参数无效: %v", err)
		}
		c.store(addr, true)
		return addr, true, nil
	}
	if envHost := os.Getenv("AGYQUOTA_HOST"); envHost != "" {
		addr, err := NormalizeHost(envHost)
		if err != nil {
			return "", true, api.Errorf(api.ErrDaemonUnreachable, "AGYQUOTA_HOST 无效: %v", err).
				WithSuggestion("请将其设置为 host[:port] 形式（如 127.0.0.1:17625）")
		}
		c.store(addr, true)
		return addr, true, nil
	}
	if addr := readAddressFile(); addr != "" {
		c.store(addr, false)
		return addr, false, nil
	}
	c.store(DefaultAddr, false)
	return DefaultAddr, false, nil
}

func (c *Client) store(addr string, explicit bool) {
	c.addr, c.explicit, c.resolved = addr, explicit, true
}

func (c *Client) ensureResolved() error {
	if c.resolved {
		return nil
	}
	_, _, err := c.Resolve()
	return err
}

// EnsureDaemon 确保目标 daemon 可用：Ping、buildID 握手；
// 生产构建且未显式指定地址时，连不上则自动拉起（有界等待就绪）。
func (c *Client) EnsureDaemon(ctx context.Context) error {
	if err := c.ensureResolved(); err != nil {
		return err
	}
	ping, err := c.pingAddr(ctx, c.addr, pingTimeout)
	if err == nil {
		return checkBuildID(ping.BuildID)
	}
	if c.explicit {
		return c.unreachableErr()
	}
	if !buildinfo.IsProduction() {
		return api.Errorf(api.ErrDaemonUnreachable, "agyquota daemon 未运行（开发构建不自动拉起）").
			WithSuggestion("请先运行 agyquota serve（开发双终端工作流）")
	}
	return c.spawnDaemon(ctx)
}

// Ping 探测 daemon 并返回其身份信息（500ms 超时）。
func (c *Client) Ping(ctx context.Context) (*api.PingResponse, error) {
	if err := c.ensureResolved(); err != nil {
		return nil, err
	}
	return c.pingAddr(ctx, c.addr, pingTimeout)
}

// GetQuota 查询配额快照；对空闲退出竞态做一次有界重试（见 retryable）。
func (c *Client) GetQuota(ctx context.Context, req api.QuotaRequest, progress func(string)) (*api.Snapshot, error) {
	if err := c.ensureResolved(); err != nil {
		return nil, err
	}
	data, err := c.quotaCall(ctx, api.EndpointQuotaGet, req, progress)
	if err != nil && c.retryable(ctx, err) {
		if e2 := c.EnsureDaemon(ctx); e2 == nil {
			data, err = c.quotaCall(ctx, api.EndpointQuotaGet, req, progress)
		}
	}
	if err != nil {
		return nil, err
	}
	var snap api.Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, api.Errorf(api.ErrInternal, "解析配额快照失败: %v", err)
	}
	return &snap, nil
}

// GetRaw 获取配额接口原始响应（--raw）。
func (c *Client) GetRaw(ctx context.Context, req api.QuotaRequest, progress func(string)) (json.RawMessage, error) {
	if err := c.ensureResolved(); err != nil {
		return nil, err
	}
	data, err := c.quotaCall(ctx, api.EndpointQuotaRaw, req, progress)
	if err != nil && c.retryable(ctx, err) {
		if e2 := c.EnsureDaemon(ctx); e2 == nil {
			data, err = c.quotaCall(ctx, api.EndpointQuotaRaw, req, progress)
		}
	}
	if err != nil {
		return nil, err
	}
	var out api.RawResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, api.Errorf(api.ErrInternal, "解析原始响应失败: %v", err)
	}
	return out.Raw, nil
}

// Stop 请求 daemon 优雅退出；daemon 未运行时返回 (false, nil)。
func (c *Client) Stop(ctx context.Context) (bool, error) {
	if err := c.ensureResolved(); err != nil {
		return false, err
	}
	if _, err := c.pingAddr(ctx, c.addr, pingTimeout); err != nil {
		return false, nil
	}
	ctx2, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx2, http.MethodPost, "http://"+c.addr+api.EndpointStop, strings.NewReader("{}"))
	if err != nil {
		return false, api.Errorf(api.ErrInternal, "构造 stop 请求失败: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return false, api.Errorf(api.ErrDaemonUnreachable, "停止 daemon 失败: %v", err)
	}
	defer resp.Body.Close()
	env, err := api.ReadEnvelope(resp.Body)
	if err != nil {
		return false, api.Errorf(api.ErrInternal, "解析 stop 响应失败: %v", err)
	}
	if !env.OK || env.Error != nil {
		if env.Error != nil {
			return false, env.Error
		}
		return false, api.Errorf(api.ErrInternal, "stop 响应不是成功包络")
	}
	var out api.StopResponse
	if err := json.Unmarshal(env.Data, &out); err != nil {
		return false, api.Errorf(api.ErrInternal, "解析 stop 响应失败: %v", err)
	}
	return out.Stopped, nil
}

// retryable 判断是否允许「空闲退出竞态」的一次重试：
// 错误为 daemon_unreachable（传输失败或 503 包络）、生产构建、未显式指定地址且请求未被取消。
// quota 两类请求只读，重试安全。
func (c *Client) retryable(ctx context.Context, err error) bool {
	var ae *api.Error
	if !errors.As(err, &ae) || ae.Code != api.ErrDaemonUnreachable {
		return false
	}
	return !c.explicit && buildinfo.IsProduction() && ctx.Err() == nil
}

func (c *Client) unreachableErr() *api.Error {
	return api.Errorf(api.ErrDaemonUnreachable, "无法连接 agyquota daemon (%s)", c.addr).
		WithSuggestion("请确认 daemon 正在运行，或检查 --host / AGYQUOTA_HOST 指向是否正确")
}

func checkBuildID(serverID string) error {
	if serverID == buildinfo.BuildID {
		return nil
	}
	return api.Errorf(api.ErrBuildMismatch,
		"buildID 不一致：客户端 %s / 服务端 %s，请运行 agyquota stop 后重试",
		buildinfo.BuildID, serverID)
}

// spawnDaemon 用自身 exe 以 detached 方式拉起 `serve --idle-timeout 30m`，
// 就有界轮询就绪（候选每轮重读地址文件 + 默认端口兜底，自愈陈旧地址）。
func (c *Client) spawnDaemon(ctx context.Context) error {
	exe, err := os.Executable()
	if err != nil {
		return api.Errorf(api.ErrDaemonStartFailed, "无法获取自身可执行文件路径: %v", err)
	}
	logPath := appdata.LogFile()
	logFile, logErr := rotateAndOpenLog(logPath)

	cmd := exec.Command(exe, "serve", "--idle-timeout", spawnIdleTimeout)
	configureDetached(cmd)
	if logErr == nil && logFile != nil {
		cmd.Stdout = logFile
		cmd.Stderr = logFile
	}
	if err := cmd.Start(); err != nil {
		if logFile != nil {
			_ = logFile.Close()
		}
		return api.Errorf(api.ErrDaemonStartFailed, "自动拉起 daemon 失败: %v", err).
			WithSuggestion("可手动运行 agyquota serve 排查")
	}
	// 子进程已持有自己的句柄；父进程仅异步回收，绝不同步 Wait
	// （否则会被 detached daemon 的生命周期拖住）。
	if logFile != nil {
		_ = logFile.Close()
	}
	go func() { _ = cmd.Wait() }()

	deadline := time.Now().Add(startWait)
	for time.Now().Before(deadline) {
		for _, cand := range c.candidates() {
			ping, err := c.pingAddr(ctx, cand, pingTimeout)
			if err != nil {
				continue
			}
			c.store(cand, false)
			return checkBuildID(ping.BuildID)
		}
		select {
		case <-ctx.Done():
			return api.Errorf(api.ErrDaemonStartFailed, "自动拉起 daemon 已取消: %v", ctx.Err())
		case <-time.After(startPollInterval):
		}
	}
	return api.Errorf(api.ErrDaemonStartFailed, "自动拉起 daemon 后 %s 内未就绪", startWait).
		WithSuggestion(fmt.Sprintf("请查看日志 %s，或手动运行 agyquota serve 排查", logPath))
}

// candidates 是就绪轮询候选集合：每轮重读地址文件（新 daemon 会原子覆盖
// 陈旧地址）并叠加默认端口。
func (c *Client) candidates() []string {
	out := make([]string, 0, 2)
	if addr := readAddressFile(); addr != "" {
		out = append(out, addr)
	}
	if !contains(out, DefaultAddr) {
		out = append(out, DefaultAddr)
	}
	return out
}

func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

// readAddressFile 只读地址文件中的 addr；缺失或损坏按未发现处理。
func readAddressFile() string {
	path := appdata.AddressFile()
	if path == "" {
		return ""
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var rec struct {
		Addr string `json:"addr"`
	}
	if err := json.Unmarshal(b, &rec); err != nil {
		return ""
	}
	return rec.Addr
}

// rotateAndOpenLog 打开 daemon 日志（追加）；超过 1 MiB 先轮转为 .1（覆盖旧档）。
// 单次运行内不再轮转，运行期日志无硬上限。
func rotateAndOpenLog(path string) (*os.File, error) {
	if path == "" {
		return nil, errors.New("数据目录不可用")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if st, err := os.Stat(path); err == nil && st.Size() > 1<<20 {
		_ = os.Rename(path, path+".1")
	}
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
}

// pingAddr 请求 /v1/ping 并解析包络。
func (c *Client) pingAddr(ctx context.Context, addr string, timeout time.Duration) (*api.PingResponse, error) {
	ctx2, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx2, http.MethodGet, "http://"+addr+api.EndpointPing, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ping 返回 HTTP %d", resp.StatusCode)
	}
	env, err := api.ReadEnvelope(resp.Body)
	if err != nil {
		return nil, err
	}
	if !env.OK || env.Error != nil {
		if env.Error != nil {
			return nil, env.Error
		}
		return nil, errors.New("ping 响应不是成功包络")
	}
	var p api.PingResponse
	if err := json.Unmarshal(env.Data, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// quotaCall 发起一次 quota 请求：优先 SSE（服务端支持时），否则按整体包络解析。
func (c *Client) quotaCall(ctx context.Context, endpoint string, req api.QuotaRequest, progress func(string)) (json.RawMessage, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, api.Errorf(api.ErrInternal, "编码请求失败: %v", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+c.addr+endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, api.Errorf(api.ErrInternal, "构造请求失败: %v", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set(api.HeaderBuild, buildinfo.BuildID)
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := c.hc.Do(httpReq)
	if err != nil {
		return nil, api.Errorf(api.ErrDaemonUnreachable, "无法连接 agyquota daemon (%s): %v", c.addr, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// 非 200 时若体为包络则透传 code（如 400 bad_args、503 daemon_unreachable）。
		if env, rerr := api.ReadEnvelope(resp.Body); rerr == nil && env.Error != nil {
			return nil, env.Error
		}
		return nil, api.Errorf(api.ErrInternal, "daemon 返回 HTTP %d", resp.StatusCode)
	}
	if strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		return readSSE(resp.Body, progress)
	}
	env, err := api.ReadEnvelope(resp.Body)
	if err != nil {
		return nil, api.Errorf(api.ErrInternal, "解析 daemon 响应失败: %v", err)
	}
	if !env.OK {
		if env.Error != nil {
			return nil, env.Error
		}
		return nil, api.Errorf(api.ErrInternal, "daemon 返回失败包络但缺少 error 字段")
	}
	return env.Data, nil
}

// readSSE 解析 daemon 的 SSE 流：progress 帧交回调，result 帧返回包络 data。
// 大帧（如 4MiB 的 --raw）由 bufio.Reader 自动扩容承接。
func readSSE(r io.Reader, progress func(string)) (json.RawMessage, error) {
	reader := bufio.NewReader(r)
	var (
		event string
		data  []string
	)
	for {
		line, err := reader.ReadString('\n')
		if err != nil && line == "" {
			if errors.Is(err, io.EOF) {
				return nil, api.Errorf(api.ErrDaemonUnreachable, "daemon 连接在返回结果前中断")
			}
			return nil, api.Errorf(api.ErrDaemonUnreachable, "读取 daemon 流失效: %v", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			switch event {
			case "result":
				env, derr := api.ReadEnvelope(strings.NewReader(strings.Join(data, "\n")))
				if derr != nil {
					return nil, api.Errorf(api.ErrInternal, "解析 daemon 结果包络失败: %v", derr)
				}
				if !env.OK {
					if env.Error != nil {
						return nil, env.Error
					}
					return nil, api.Errorf(api.ErrInternal, "daemon 返回失败包络但缺少 error 字段")
				}
				return env.Data, nil
			case "progress":
				if progress != nil {
					var p struct {
						Message string `json:"message"`
					}
					if json.Unmarshal([]byte(strings.Join(data, "\n")), &p) == nil && p.Message != "" {
						progress(p.Message)
					}
				}
			}
			event, data = "", nil
			continue
		}
		switch {
		case strings.HasPrefix(line, "event:"):
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			v := strings.TrimPrefix(line, "data:")
			v = strings.TrimPrefix(v, " ")
			data = append(data, v)
		}
	}
}
