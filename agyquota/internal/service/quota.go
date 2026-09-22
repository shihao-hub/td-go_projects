package service

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"agyquota/internal/agapi"
)

// 模型桶名称（与 Antigravity 设置面板与 agy 保持一致）。
const (
	bucketGemini = "Gemini Models"
	bucketClaude = "Claude and GPT models"
	bucketOther  = "Other models"
)

var bucketOrder = map[string]int{bucketGemini: 0, bucketClaude: 1, bucketOther: 2}

// parseSnapshot 把 agy 的原始输出归一化为模型桶 × 窗口快照。
func parseSnapshot(raw json.RawMessage, now time.Time) (*Snapshot, error) {
	var usage agapi.AgyUsageOutput
	if err := json.Unmarshal(raw, &usage); err != nil {
		return nil, fmt.Errorf("响应不是预期的 JSON 格式: %w", err)
	}

	groups := usage.Command.Data.Groups
	if len(groups) == 0 {
		// 容错：有些版本可能顶层直接返回 AgyUsageData
		var directData agapi.AgyUsageData
		if err := json.Unmarshal(raw, &directData); err == nil && len(directData.Groups) > 0 {
			groups = directData.Groups
		}
	}

	var buckets []Bucket
	for _, group := range groups {
		var windows []Window
		for _, b := range group.Buckets {
			rem := b.RemainingFraction
			winID, label := normalizeWindowID(b.Window, b.ID, b.Name)
			resetAt, resetsIn := describeResetTime(b.ResetTime, now)

			windows = append(windows, Window{
				Window:            winID,
				Label:             label,
				Percent:           math.Round(rem*1000) / 10,
				RemainingFraction: &rem,
				ResetAt:           resetAt,
				ResetsIn:          resetsIn,
			})
		}
		sortWindows(windows)

		// 归类桶名称
		bName := group.Name
		if bName == "" {
			bName = bucketOther
		}

		// 在 agy 结构中，每个 group 下共享配额桶，将它作为一个代表性的 ModelQuota
		buckets = append(buckets, Bucket{
			Name: bName,
			Models: []ModelQuota{
				{
					Name:    group.Name,
					Windows: windows,
				},
			},
		})
	}

	sort.SliceStable(buckets, func(i, j int) bool {
		oi, oki := bucketOrder[buckets[i].Name]
		if !oki {
			oi = 99
		}
		oj, okj := bucketOrder[buckets[j].Name]
		if !okj {
			oj = 99
		}
		return oi < oj
	})

	return &Snapshot{
		FetchedAt: now,
		Buckets:   buckets,
	}, nil
}

// normalizeWindowID 归一化窗口标识与标签。
func normalizeWindowID(raws ...string) (id, label string) {
	var raw string
	for _, r := range raws {
		if r != "" {
			raw = r
			break
		}
	}
	if raw == "" {
		return "quota", "quota"
	}
	l := strings.ToLower(raw)
	switch {
	case strings.Contains(l, "five") || strings.Contains(l, "5h") || strings.Contains(l, "pt5h"):
		return "five_hour", "5h"
	case strings.Contains(l, "week") || strings.Contains(l, "p1w") || strings.Contains(l, "7d"):
		return "weekly", "weekly"
	case strings.Contains(l, "day") || strings.Contains(l, "24h"):
		return "daily", "daily"
	default:
		return l, l
	}
}

// describeResetTime 解析重置时间。
func describeResetTime(resetStr string, now time.Time) (string, string) {
	if resetStr == "" {
		return "", ""
	}
	t, err := time.Parse(time.RFC3339, resetStr)
	if err != nil {
		return resetStr, ""
	}
	return resetStr, humanizeIn(t.Sub(now))
}

// humanizeIn 人读剩余时间："6天21小时后刷新"。
func humanizeIn(d time.Duration) string {
	if d <= 0 {
		return "已可刷新"
	}
	days := int(d / (24 * time.Hour))
	hours := int(d % (24 * time.Hour) / time.Hour)
	mins := int(d % time.Hour / time.Minute)
	switch {
	case days > 0 && hours > 0:
		return fmt.Sprintf("%d天%d小时后刷新", days, hours)
	case days > 0:
		return fmt.Sprintf("%d天后刷新", days)
	case hours > 0 && mins > 0:
		return fmt.Sprintf("%d小时%d分钟后刷新", hours, mins)
	case hours > 0:
		return fmt.Sprintf("%d小时后刷新", hours)
	default:
		return fmt.Sprintf("%d分钟后刷新", mins)
	}
}

// sortWindows 窗口排序：five_hour → weekly → daily → 其余按标识。
func sortWindows(ws []Window) {
	rank := func(w Window) int {
		switch w.Window {
		case "five_hour":
			return 0
		case "weekly":
			return 1
		case "daily":
			return 2
		default:
			return 3
		}
	}
	sort.SliceStable(ws, func(i, j int) bool { return rank(ws[i]) < rank(ws[j]) })
}
