// Package agapi 提供 Antigravity CLI (agy) 与 Zed ACP 的调用与数据获取封装。
package agapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	agyUsageMaxAttempts = 3
	agyUsageInitialWait = 1 * time.Second
	agyUsageMaxWait     = 2 * time.Second
)

var agyUsageRetryDelays = [...]time.Duration{agyUsageInitialWait, agyUsageMaxWait}

type agyExecutionError struct {
	err       error
	retryable bool
}

func (e *agyExecutionError) Error() string {
	return e.err.Error()
}

func (e *agyExecutionError) Unwrap() error {
	return e.err
}

func isRetryableQuotaError(errMsg string) bool {
	lower := strings.ToLower(errMsg)
	quotaSummaryEOF := strings.Contains(lower, "retrieve user quota summary") && strings.Contains(lower, "eof")
	return quotaSummaryEOF ||
		strings.Contains(lower, "unexpected end of json input") ||
		strings.Contains(lower, "unexpected eof") ||
		strings.Contains(lower, "io: unexpected eof") ||
		strings.Contains(lower, "connection reset")
}

// UsageResult 封装查询返回的数据与账号元信息。
type UsageResult struct {
	Raw       []byte
	Account   string     // 账号邮箱（若可获取）
	ExpiresAt *time.Time // 凭据到期时间（若适用）
}

// AgyUsageOutput 是 agy -p "/usage" --output-format json 的完整应答结构。
type AgyUsageOutput struct {
	Status   string `json:"status"`
	Response string `json:"response"`
	Command  struct {
		Name string       `json:"name"`
		Data AgyUsageData `json:"data"`
	} `json:"command"`
}

// AgyUsageData 包含 groups 数组。
type AgyUsageData struct {
	Description string          `json:"description"`
	Groups      []AgyUsageGroup `json:"groups"`
}

// AgyUsageGroup 单个模型分组（如 Gemini Models、Claude and GPT models）。
type AgyUsageGroup struct {
	Name        string           `json:"name"`
	Description string           `json:"description"`
	Buckets     []AgyUsageBucket `json:"buckets"`
}

// AgyUsageBucket 单个配额桶（如 5h、weekly）。
type AgyUsageBucket struct {
	ID                string  `json:"id"`
	Name              string  `json:"name"`
	Description       string  `json:"description"`
	Window            string  `json:"window"`
	RemainingFraction float64 `json:"remaining_fraction"`
	ResetTime         string  `json:"reset_time"`
}

// Client 是通过调用本机 agy 命令行获取配额的客户端。
type Client struct {
	AgyPath string
	Logf    func(format string, args ...any)
}

// NewClient 创建 agapi 客户端，自动探测 agy 可执行文件路径。
func NewClient() *Client {
	return &Client{
		AgyPath: findAgyPath(),
	}
}

// findAgyPath 自动探测 agy 可执行文件路径。
func findAgyPath() string {
	if p, err := exec.LookPath("agy"); err == nil {
		return p
	}
	if p, err := exec.LookPath("agy.exe"); err == nil {
		return p
	}
	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData != "" {
		candidate := filepath.Join(localAppData, "agy", "bin", "agy.exe")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return "agy"
}

// FetchUsageWithMeta 获取配额数据并提取 agy 登录邮箱。
func (c *Client) FetchUsageWithMeta(ctx context.Context) (*UsageResult, error) {
	raw, err := c.FetchUsage(ctx)
	if err != nil {
		return nil, err
	}
	email := GetAgyLoggedEmail()
	return &UsageResult{
		Raw:     raw,
		Account: email,
	}, nil
}

// FetchUsage 调用 agy 获取 /usage 并返回原始 JSON 字节。
// Windows 下通过 ensureSilentAgyExe 生成或复用 Subsystem==2 的静默副本，杜绝 DefTerm 抢焦点；
// 失败时禁止回退原始 agy.exe，直接返回明确错误。
// 启动日志真实打印实际的 runPath 并明确显示是否使用了 agy_silent.exe。
func (c *Client) FetchUsage(ctx context.Context) ([]byte, error) {
	if c.AgyPath == "" {
		return nil, errors.New("未找到 agy 命令行工具，请先安装 Antigravity CLI")
	}

	runPath, err := ensureSilentAgyExe(c.AgyPath)
	if err != nil {
		return nil, fmt.Errorf("准备静默 agy 副本失败: %w", err)
	}

	isSilent := filepath.Base(runPath) == "agy_silent.exe"
	if c.Logf != nil {
		c.Logf("正在执行 %s -p \"/usage\" --output-format json (使用静默副本: %t) ...", runPath, isSilent)
	}

	for attempt := 1; attempt <= agyUsageMaxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("执行 agy 命令失败: %v", err)
		}

		raw, err := c.fetchUsageOnce(ctx, runPath)
		if err == nil {
			return raw, nil
		}

		var executionErr *agyExecutionError
		if !errors.As(err, &executionErr) || !executionErr.retryable || attempt == agyUsageMaxAttempts {
			return nil, err
		}

		delay := agyUsageRetryDelays[attempt-1]
		if c.Logf != nil {
			c.Logf("agy 配额接口出现瞬态错误，第 %d 次尝试失败，将在 %s 后重试 ...", attempt, delay)
		}
		if err := waitAgyRetry(ctx, delay); err != nil {
			return nil, err
		}
	}

	return nil, errors.New("agy 配额查询失败")
}

// fetchUsageOnce 执行一次 agy 查询；进程、管道和响应校验均在此处完成。
func (c *Client) fetchUsageOnce(ctx context.Context, runPath string) ([]byte, error) {
	p, err := startAgyProcess(runPath)
	if err != nil {
		return nil, err
	}
	defer p.shutdown()

	var stdout, stderr bytes.Buffer
	copyDone := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(&stdout, p.Stdout()); copyDone <- struct{}{} }()
	go func() { _, _ = io.Copy(&stderr, p.Stderr()); copyDone <- struct{}{} }()

	waitDone := make(chan struct{})
	var exitCode uint32
	var waitErr error
	go func() { exitCode, waitErr = p.Wait(); close(waitDone) }()

	// 超时/取消：终止整树解除 Wait 阻塞。
	select {
	case <-waitDone:
	case <-ctx.Done():
		p.shutdown()
		<-waitDone
		return nil, fmt.Errorf("执行 agy 命令失败: %v", ctx.Err())
	}
	<-copyDone
	<-copyDone

	if waitErr != nil {
		return nil, &agyExecutionError{
			err:       fmt.Errorf("执行 agy 命令失败 (%v): %s", waitErr, stderr.String()),
			retryable: false,
		}
	}
	if exitCode != 0 {
		errMsg := stderr.String()
		if errMsg == "" {
			errMsg = stdout.String()
		}
		return nil, &agyExecutionError{
			err:       fmt.Errorf("执行 agy 命令失败 (exit status %d): %s", exitCode, errMsg),
			retryable: isRetryableQuotaError(errMsg),
		}
	}

	outBytes := stdout.Bytes()
	var parsed AgyUsageOutput
	if err := json.Unmarshal(outBytes, &parsed); err != nil {
		return nil, fmt.Errorf("解析 agy 输出格式失败: %w", err)
	}
	if parsed.Status != "SUCCESS" && parsed.Status != "" {
		return nil, fmt.Errorf("agy 返回非成功状态: %s", parsed.Status)
	}

	return outBytes, nil
}

func waitAgyRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer func() {
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
	}()

	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
