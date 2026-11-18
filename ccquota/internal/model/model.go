// Package model 定义 CLI、WebUI 与后续 GUI 共用的额度数据结构。
package model

import "time"

// Tier 标识一个限额窗口。
const (
	TierFiveHour = "five_hour"
	TierWeekly   = "weekly"
	TierMonthly  = "monthly"
)

// 卡片类型。
const (
	KindCodingPlan   = "coding_plan"
	KindBalance      = "balance"
	KindSubscription = "subscription"
)

// Tier 是一个限额窗口的用量。Utilization 为已用百分比（0-100），与 cc-switch 语义一致。
type Tier struct {
	Key         string     `json:"key"`
	// Label 是可选的显示名（如 Antigravity 的模型分组）；为空时由 Key 推导。
	Label       string     `json:"label,omitempty"`
	Utilization float64    `json:"utilization"`
	ResetsAt    *time.Time `json:"resetsAt,omitempty"`
}

// Balance 是账户余额（仅余额型供应商）。
type Balance struct {
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
}

// Card 是一张供应商额度卡片。
type Card struct {
	App        string    `json:"app"`
	ProviderID string    `json:"providerId"`
	Name       string    `json:"name"`
	BaseURL    string    `json:"baseUrl"`
	Kind       string    `json:"kind"`
	Current    bool      `json:"current"`
	OK         bool      `json:"ok"`
	Tiers      []Tier    `json:"tiers,omitempty"`
	Balance    *Balance  `json:"balance,omitempty"`
	Err        string    `json:"err,omitempty"`
	QueriedAt  time.Time `json:"queriedAt"`
}

// Remaining 返回某档的剩余百分比（已用换算）。
func (t Tier) Remaining() float64 {
	remaining := 100 - t.Utilization
	if remaining < 0 {
		return 0
	}
	if remaining > 100 {
		return 100
	}
	return remaining
}
