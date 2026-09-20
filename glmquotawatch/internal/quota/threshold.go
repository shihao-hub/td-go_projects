// Package quota 实现用量阈值告警的纯逻辑状态机与展示格式化。
// 不做任何 IO——输入配置/旧状态/窗口数据，输出新状态与逐窗口评估结果，
// 便于表驱动测试。
package quota

import (
	"fmt"
	"sort"
	"time"

	"glmquotawatch/internal/api"
	"glmquotawatch/internal/store"
)

// AlertTitle 阈值告警通知标题。
const AlertTitle = "GLM 用量告警"

// Outcome 单个窗口在本次采样中的评估结果。
type Outcome struct {
	Key        string // 窗口稳定键 Type:Unit:Number
	Type       string
	Label      string // 窗口人读标签，如 "5h 窗口"
	Percentage int64
	Notify     bool          // 是否新触发档位（需要发通知）
	Highest    int           // 本次新触发的最高档位
	Message    string        // 通知正文（Notify=true 时非空）
	ResetIn    time.Duration // 距下次刷新时长（HasReset=true 时有效，可能为负）
	HasReset   bool          // 上游是否给出 nextResetTime
	Cleared    bool          // 本轮发生滞回清空（重新武装）
}

// Evaluate 评估全部 TOKENS_LIMIT 窗口，返回应持久化的新状态与逐窗口结果。
//
// 规则：
//   - F = {t ∈ thresholds : t <= p}（含等号）；newly = F 中未通知过的档位，
//     非空则通知一次（只报 max(newly)），但记账 notified ∪= F 全量——
//     一轮从 48% 跳到 92% 只响一声，回落判断语义正确；
//   - 滞回清空：p < min(thresholds) - hysteresis 时清空该窗口已通知档位
//     （滚动窗口用量自然回落不代表已处理，只有跌破滞回线才重新武装）；
//   - prev 的阈值指纹与当前配置不一致时整体清空（配置变更即重新武装）。
func Evaluate(cfg store.Config, prev store.State, limits []api.Limit, now time.Time) (store.State, []Outcome) {
	next := store.State{
		Version:    1,
		Thresholds: append([]int(nil), cfg.Thresholds...),
		Notified:   map[string][]int{},
		UpdatedAt:  now,
	}
	if prev.Version != 0 && equalInts(prev.Thresholds, cfg.Thresholds) {
		for k, v := range prev.Notified {
			next.Notified[k] = v
		}
	}

	clearBelow := 0
	if len(cfg.Thresholds) > 0 {
		clearBelow = cfg.Thresholds[0] - cfg.Hysteresis
	}

	var outs []Outcome
	for _, l := range limits {
		if l.Type != "TOKENS_LIMIT" || l.Percentage == nil {
			continue // 一期只评估 token 类窗口；percentage 缺失无法评估
		}
		p := *l.Percentage
		key := l.Key()
		out := Outcome{
			Key:        key,
			Type:       l.Type,
			Label:      WindowLabel(l),
			Percentage: p,
		}
		if l.NextResetTime != nil && *l.NextResetTime > 0 {
			out.HasReset = true
			out.ResetIn = time.UnixMilli(*l.NextResetTime).Sub(now)
		}

		notified := next.Notified[key]
		newly, highest := 0, 0
		for _, th := range cfg.Thresholds {
			if int64(th) <= p && !containsInt(notified, th) {
				newly++
				if th > highest {
					highest = th
				}
			}
		}
		switch {
		case newly > 0:
			out.Notify = true
			out.Highest = highest
			out.Message = fmt.Sprintf("%s已消耗 %d%%（阈值 %d%%）", out.Label, p, highest)
			if out.HasReset && out.ResetIn > 0 {
				out.Message += "，距下次刷新还有 " + FormatResetIn(out.ResetIn)
			}
			// 记账 F 全量（不只 newly），回落时不需要重复武装已越过的档位
			merged := append([]int(nil), notified...)
			for _, th := range cfg.Thresholds {
				if int64(th) <= p && !containsInt(merged, th) {
					merged = append(merged, th)
				}
			}
			sort.Ints(merged)
			next.Notified[key] = merged
		case p < int64(clearBelow):
			out.Cleared = true
			delete(next.Notified, key)
		}
		outs = append(outs, out)
	}
	return next, outs
}

func containsInt(list []int, v int) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
