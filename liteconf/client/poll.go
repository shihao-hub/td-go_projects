package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"strings"
	"time"
)

// 退避参数：1s 起步、×2、上限 30s
const (
	baseBackoff   = time.Second
	maxBackoff    = 30 * time.Second
	maxWatchPolls = 8 << 20
)

// run 长轮询主循环：携带本地版本请求 watch 端点，
// 版本变化时拉取最新配置、替换缓存、派发回调；
// 失败按指数退避（含随机抖动）重试，ctx 取消退出。
func (c *Client) run(ctx context.Context) {
	backoff := baseBackoff
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		changed, err := c.pollOnce(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			sleep := backoff + backoff/2 + time.Duration(rand.Int64N(int64(backoff)))
			slog.Warn("liteconf: poll failed, retry with backoff",
				"app", c.cfg.App, "env", c.cfg.Env, "err", err, "sleep", sleep)
			if !sleepCtx(ctx, sleep) {
				return
			}
			backoff = min(backoff*2, maxBackoff)
			continue
		}
		_ = changed
		backoff = baseBackoff
	}
}

// pollOnce 单轮：watch 长轮询 → 版本变化则拉取最新并 swap + dispatch
func (c *Client) pollOnce(ctx context.Context) (bool, error) {
	version, err := c.watchOnce(ctx)
	if err != nil {
		return false, err
	}
	if version == c.cache.version() {
		// 超时未变化（server 返回当前版本），直接下一轮
		return false, nil
	}
	content, newVersion, err := c.fetch(ctx)
	if err != nil {
		return false, err
	}
	if newVersion == c.cache.version() {
		return false, nil
	}
	old := c.Snapshot()
	c.cache.swap(content, newVersion)
	c.dispatch(old, content)
	slog.Info("liteconf: config updated", "app", c.cfg.App, "env", c.cfg.Env, "version", newVersion)
	return true, nil
}

// watchOnce 发起一次长轮询请求，返回 server 侧当前版本。
// server 默认挂起 30s；版本已更新/被唤醒/超时都会返回最新版本。
func (c *Client) watchOnce(ctx context.Context) (uint64, error) {
	url := fmt.Sprintf("%s/api/watch/%s/%s?version=%d",
		strings.TrimRight(c.cfg.ServerURL, "/"), c.cfg.App, c.cfg.Env, c.cache.version())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxRespSize))
	if err != nil {
		return 0, err
	}
	var ar apiResponse
	if err := json.Unmarshal(body, &ar); err != nil {
		return 0, fmt.Errorf("decode watch response (status %d): %w", resp.StatusCode, err)
	}
	if resp.StatusCode != http.StatusOK || ar.Code != "ok" {
		return 0, fmt.Errorf("watch server error (status %d): code=%s message=%s",
			resp.StatusCode, ar.Code, ar.Message)
	}
	return ar.Data.Version, nil
}

// sleepCtx 可中断睡眠：ctx 取消返回 false
func sleepCtx(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
