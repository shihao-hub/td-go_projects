// demo.go：演示模式模拟上游——60 倍速时间压缩（5 小时窗口 → 5 分钟真实时间），
// 百分比从 0% 线性增长到 100%，让用户在 5 分钟内完整体验跨档 Toast、
// 托盘与仪表盘变化。data 原文复用 api.Usage 类型序列化（键名与真实上游
// camelCase 天然一致），samples 落盘 → ReadHistory → buildStatusView 全链路闭环。
package api

import (
	"context"
	"encoding/json"
	"time"
)

// Demo 时序常量：真实时长 / 虚拟窗口时长 / 压缩比。
const (
	DemoRealDuration  = 5 * time.Minute // 真实时间 5 分钟
	DemoVirtualWindow = 5 * time.Hour   // 模拟 5 小时 token 窗口
)

// DemoFetcher 演示数据源：按 start 起算的已流逝真实时间线性推进百分比。
type DemoFetcher struct {
	Start time.Time
}

// NewDemoFetcher 以当前时刻为起点构造。
func NewDemoFetcher() *DemoFetcher {
	return &DemoFetcher{Start: time.Now()}
}

// FetchUsage 返回模拟用量：单 TOKENS_LIMIT 5h 窗口，percentage = elapsed/总时长×100。
func (d *DemoFetcher) FetchUsage(_ context.Context) (*Usage, json.RawMessage, error) {
	now := time.Now()
	elapsed := now.Sub(d.Start)
	if elapsed < 0 {
		elapsed = 0
	}
	// 5h 虚拟窗口压缩进 5min：pct = elapsed/300s * 100，封顶 100
	pct := int64(elapsed * 100 / DemoRealDuration)
	if pct > 100 {
		pct = 100
	}
	nextReset := d.Start.Add(DemoRealDuration).UnixMilli()
	u := &Usage{
		Level: "DEMO",
		Limits: []Limit{{
			Type:          "TOKENS_LIMIT",
			Unit:          3, // 小时（WindowLabel 渲染为 "5h 窗口"）
			Number:        5,
			Percentage:    &pct,
			NextResetTime: &nextReset,
		}},
	}
	raw, err := json.Marshal(u) // 复用 api 类型序列化，禁止手工拼 map
	if err != nil {
		return nil, nil, err
	}
	return u, raw, nil
}
