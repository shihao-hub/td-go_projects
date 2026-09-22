package service

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// ---- fetchAvailableModels 响应解析（防御式）----
//
// 该接口为 Google 内部接口，响应结构未正式公开。字段名依据：
//   - language_server.exe 二进制 strings（models / quotaInfo / remainingFraction /
//     resetTime / window_size）；
//   - 同源 Cloud Code 接口（gemini-cli）的已知形状。
//
// 解析尽量宽松：识别「扁平 quotaInfo」与「windows 数组」两种形态；
// 识别不了的模型/窗口跳过，不报错；原始响应可经 --raw 查看。

// 模型桶名称（与 Antigravity 设置面板保持一致）。
const (
	bucketGemini = "Gemini Models"
	bucketClaude = "Claude and GPT models"
	bucketOther  = "Other models"
)

var bucketOrder = map[string]int{bucketGemini: 0, bucketClaude: 1, bucketOther: 2}

type modelsResponse struct {
	Models map[string]modelEntry `json:"models"`
}

type modelEntry struct {
	QuotaInfo json.RawMessage `json:"quotaInfo"`
}

// windowFields 是单个窗口可能出现的全部字段（宽松匹配）。
type windowFields struct {
	RemainingFraction *float64        `json:"remainingFraction"`
	ResetTime         json.RawMessage `json:"resetTime"`
	WindowSize        string          `json:"windowSize"`
	WindowID          string          `json:"windowId"`
	Window            string          `json:"window"`
}

// quotaInfoShape 兼容两种形态：{windows: [...]} 数组，或扁平字段。
type quotaInfoShape struct {
	Windows           []windowFields  `json:"windows"`
	RemainingFraction *float64        `json:"remainingFraction"`
	ResetTime         json.RawMessage `json:"resetTime"`
	WindowSize        string          `json:"windowSize"`
	WindowID          string          `json:"windowId"`
}

// parseSnapshot 把原始响应归一化为模型桶 × 窗口快照。
// models 为空不是错误（空集合是合法结果），由入口提示。
func parseSnapshot(raw json.RawMessage, now time.Time) (*Snapshot, error) {
	var resp modelsResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("响应不是预期的 JSON 对象: %w", err)
	}
	if len(resp.Models) == 0 {
		// 回退：顶层直接是 {"<model>": {...}} 的形态
		var flat map[string]modelEntry
		if err := json.Unmarshal(raw, &flat); err == nil && len(flat) > 0 {
			resp.Models = flat
		}
	}

	buckets := map[string][]ModelQuota{}
	for name, entry := range resp.Models {
		windows := parseQuotaInfo(entry.QuotaInfo, now)
		if len(windows) == 0 {
			continue
		}
		sortWindows(windows)
		b := classify(name)
		buckets[b] = append(buckets[b], ModelQuota{Name: name, Windows: windows})
	}

	var out []Bucket
	for _, bname := range []string{bucketGemini, bucketClaude, bucketOther} {
		models, ok := buckets[bname]
		if !ok {
			continue
		}
		sort.Slice(models, func(i, j int) bool { return models[i].Name < models[j].Name })
		out = append(out, Bucket{Name: bname, Models: models})
	}
	return &Snapshot{FetchedAt: now, Buckets: out}, nil
}

// parseQuotaInfo 尽力从 quotaInfo 原始 JSON 提取窗口列表。
func parseQuotaInfo(raw json.RawMessage, now time.Time) []Window {
	if len(raw) == 0 {
		return nil
	}
	var shape quotaInfoShape
	if err := json.Unmarshal(raw, &shape); err != nil {
		return nil
	}
	var out []Window
	if len(shape.Windows) > 0 {
		for _, w := range shape.Windows {
			if win, ok := toWindow(w, now); ok {
				out = append(out, win)
			}
		}
		return out
	}
	// 扁平形态：整个 quotaInfo 就是一个窗口
	if win, ok := toWindow(windowFields{
		RemainingFraction: shape.RemainingFraction,
		ResetTime:         shape.ResetTime,
		WindowSize:        shape.WindowSize,
		WindowID:          shape.WindowID,
	}, now); ok {
		out = append(out, win)
	}
	return out
}

func toWindow(w windowFields, now time.Time) (Window, bool) {
	if w.RemainingFraction == nil {
		return Window{}, false
	}
	id, label := normalizeWindowID(w.WindowSize, w.WindowID, w.Window)
	resetAt, resetsIn := describeReset(w.ResetTime, now)
	return Window{
		Window:            id,
		Label:             label,
		Percent:           math.Round(*w.RemainingFraction*1000) / 10,
		RemainingFraction: w.RemainingFraction,
		ResetAt:           resetAt,
		ResetsIn:          resetsIn,
	}, true
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
	case strings.Contains(l, "five") || l == "5h" || l == "pt5h":
		return "five_hour", "5h"
	case strings.Contains(l, "week") || l == "p1w" || l == "7d" || l == "168h":
		return "weekly", "weekly"
	case strings.Contains(l, "day") || l == "p1d" || l == "24h":
		return "daily", "daily"
	default:
		return l, l
	}
}

// describeReset 解析重置时间：字符串优先按 RFC3339，数字按 Unix 秒。
// 返回 (规范时间或原值, 人读剩余)。
func describeReset(raw json.RawMessage, now time.Time) (string, string) {
	if len(raw) == 0 {
		return "", ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil && s != "" {
		if t, err2 := time.Parse(time.RFC3339, s); err2 == nil {
			return s, humanizeIn(time.Until(t))
		}
		return s, ""
	}
	var sec float64
	if err := json.Unmarshal(raw, &sec); err == nil && sec > 0 {
		t := time.Unix(int64(sec), 0).UTC()
		return t.Format(time.RFC3339), humanizeIn(time.Until(t))
	}
	return "", ""
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

// classify 按模型名分桶（动态识别，不硬编码模型清单）。
func classify(model string) string {
	l := strings.ToLower(model)
	switch {
	case strings.HasPrefix(l, "gemini"):
		return bucketGemini
	case strings.HasPrefix(l, "claude"), strings.HasPrefix(l, "gpt"):
		return bucketClaude
	default:
		return bucketOther
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
