package quota

import (
	"context"
	"sync"

	"ccquota/internal/ccswitch"
	"ccquota/internal/model"
)

// maxConcurrent 限制并发查询数，避免对同一上游瞬间打出多份请求。
const maxConcurrent = 4

// Eligible 判断某供应商是否应出现在额度列表。
func Eligible(p ccswitch.Provider) bool {
	switch p.AppType {
	case "kimi", "agy":
		return true
	case "codex":
		return p.AuthKind == "codex_oauth" || p.Category == "official" || p.ID == "codex-official"
	case "claude":
		return p.Usage != nil
	}
	return false
}

// Collect 并发查询所有符合条件的供应商，并用上次成功值兜底失败卡片。
func Collect(ctx context.Context, providers []ccswitch.Provider, prev map[string]model.Card) []model.Card {
	var targets []ccswitch.Provider
	for _, p := range providers {
		if Eligible(p) {
			targets = append(targets, p)
		}
	}

	results := make([]model.Card, len(targets))
	sem := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup
	for i, p := range targets {
		wg.Add(1)
		go func(i int, p ccswitch.Provider) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i] = Query(ctx, p)
		}(i, p)
	}
	wg.Wait()

	for i := range results {
		card := &results[i]
		if card.OK {
			continue
		}
		old, ok := prev[card.ProviderID]
		if !ok || !old.OK {
			continue
		}
		// 保留上次成功快照，只覆盖错误与查询时间，避免一次抖动整卡变红。
		card.Tiers = old.Tiers
		card.Balance = old.Balance
		card.QueriedAt = old.QueriedAt
	}
	return results
}
