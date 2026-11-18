package quota

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"ccquota/internal/model"
)

// agyTimeout 覆盖 Antigravity CLI 冷启动开销（实测单次数秒）。
const agyTimeout = 40 * time.Second

// agyResp 是 `agy -p "/usage" --output-format json` 的应答结构。
type agyResp struct {
	Status  string `json:"status"`
	Command struct {
		Data struct {
			Groups []struct {
				Name    string `json:"name"`
				Buckets []struct {
					ID                string  `json:"id"`
					Name              string  `json:"name"`
					Window            string  `json:"window"`
					RemainingFraction float64 `json:"remaining_fraction"`
					ResetTime         string  `json:"reset_time"`
				} `json:"buckets"`
			} `json:"groups"`
		} `json:"data"`
	} `json:"command"`
}

// findAgy 探测 agy 可执行文件路径。
func findAgy() string {
	if p, err := exec.LookPath("agy"); err == nil {
		return p
	}
	if p, err := exec.LookPath("agy.exe"); err == nil {
		return p
	}
	if local := os.Getenv("LOCALAPPDATA"); local != "" {
		candidate := filepath.Join(local, "agy", "bin", "agy.exe")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

// queryAgy 通过 Antigravity CLI 读取分组额度。
//
// 走 CLI 而非直接打 daily-cloudcode-pa：后者的返回内容取决于 User-Agent 伪装，
// 规则一变就只剩部分分组；agy 自己的 /usage 与 IDE 展示完全一致。
func queryAgy(ctx context.Context) ([]model.Tier, error) {
	path := findAgy()
	if path == "" {
		return nil, fmt.Errorf("未找到 Antigravity CLI（agy），请先安装")
	}

	ctx, cancel := context.WithTimeout(ctx, agyTimeout)
	defer cancel()

	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		tiers, err := runAgyOnce(ctx, path)
		if err == nil {
			return tiers, nil
		}
		lastErr = err
		if !isTransientAgyError(err) {
			break
		}
		select {
		case <-ctx.Done():
			return nil, lastErr
		case <-time.After(time.Duration(attempt) * time.Second):
		}
	}
	return nil, lastErr
}

func runAgyOnce(ctx context.Context, path string) ([]model.Tier, error) {
	cmd := exec.CommandContext(ctx, path, "-p", "/usage", "--output-format", "json")
	hideWindow(cmd)
	raw, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(string(raw))
		if exitErr, ok := err.(*exec.ExitError); ok {
			if stderr := strings.TrimSpace(string(exitErr.Stderr)); stderr != "" {
				msg = stderr
			}
		}
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("执行 agy 失败: %s", msg)
	}

	var resp agyResp
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("解析 agy 输出失败: %w", err)
	}
	if resp.Status != "" && resp.Status != "SUCCESS" {
		return nil, fmt.Errorf("agy 返回非成功状态: %s", resp.Status)
	}

	var tiers []model.Tier
	for _, group := range resp.Command.Data.Groups {
		// 组内按 5 小时 → 周 → 月的固定顺序输出（上游 buckets 顺序不稳定）。
		byKey := make(map[string]model.Tier, len(group.Buckets))
		for _, bucket := range group.Buckets {
			key := agyWindowKey(bucket.Window)
			if key == "" {
				continue
			}
			tier := remainToTier(key, bucket.RemainingFraction, parseFlexTime(bucket.ResetTime))
			tier.Label = group.Name
			byKey[key] = tier
		}
		for _, key := range []string{model.TierFiveHour, model.TierWeekly, model.TierMonthly} {
			if tier, ok := byKey[key]; ok {
				tiers = append(tiers, tier)
			}
		}
	}
	return tiers, nil
}

// agyWindowKey 把 agy 的窗口标识映射成统一档位名。
func agyWindowKey(window string) string {
	switch strings.ToLower(strings.TrimSpace(window)) {
	case "5h", "five_hour", "5hour":
		return model.TierFiveHour
	case "weekly", "week":
		return model.TierWeekly
	case "monthly", "month":
		return model.TierMonthly
	}
	return ""
}

// isTransientAgyError 判定是否值得重试（EOF 类瞬态中断）。
func isTransientAgyError(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "eof") ||
		strings.Contains(msg, "unexpected end") ||
		strings.Contains(msg, "connection reset")
}
