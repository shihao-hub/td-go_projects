// fetcher.go：数据源抽象。service 层依赖本接口而非具体 Client，
// 演示模式注入 DemoFetcher，测试注入 fake，真实模式用 Client。
package api

import (
	"context"
	"encoding/json"
)

// Fetcher 用量数据源接口：返回解析结果与 data 字段原文（供历史落盘）。
type Fetcher interface {
	FetchUsage(ctx context.Context) (*Usage, json.RawMessage, error)
}
