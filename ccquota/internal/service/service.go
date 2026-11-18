// Package service 是额度聚合的公共用例层：读 cc-switch、查询、缓存、回退旧值。
// CLI、WebUI 与后续 GUI 都只通过本包取数据，壳层不写业务逻辑。
package service

import (
	"context"
	"sort"
	"time"

	"ccquota/internal/ccswitch"
	"ccquota/internal/model"
	"ccquota/internal/quota"
	"ccquota/internal/store"
)

// DefaultTTL 与 cc-switch 默认的 autoQueryInterval（5 分钟）对齐。
const DefaultTTL = 5 * time.Minute

// Options 控制一次取数。
type Options struct {
	// Refresh 为真时忽略缓存，强制访问上游。
	Refresh bool
	// TTL 是缓存有效期。
	TTL time.Duration
	// App 非空时只保留该 app_type 的卡片。
	App string
	// All 为真时显示全部启用额度查询的供应商；否则每个应用只留一张
	//（优先「使用中」，没有则取第一张）。
	All bool
}

// Result 是一次取数的结果。
type Result struct {
	Cards     []model.Card `json:"cards"`
	UpdatedAt time.Time    `json:"updatedAt"`
	FromCache bool         `json:"fromCache"`
}

// Service 持有只读数据源配置。
type Service struct {
	DBPath string
}

// New 创建 Service；DBPath 为空时使用默认位置。
func New(dbPath string) (*Service, error) {
	if dbPath == "" {
		p, err := ccswitch.DefaultDBPath()
		if err != nil {
			return nil, err
		}
		dbPath = p
	}
	return &Service{DBPath: dbPath}, nil
}

// Cards 返回额度卡片；命中缓存时直接返回，失败卡片沿用上次成功值。
func (s *Service) Cards(ctx context.Context, opts Options) (Result, error) {
	ttl := opts.TTL
	if ttl == 0 {
		ttl = DefaultTTL
	}

	cached, hasCache := store.Load()
	if !opts.Refresh && hasCache && store.Fresh(cached, ttl) {
		return Result{Cards: applyView(cached.Cards, opts), UpdatedAt: cached.UpdatedAt, FromCache: true}, nil
	}

	providers, err := ccswitch.Load(s.DBPath)
	if err != nil {
		return Result{}, err
	}
	prev := map[string]model.Card{}
	if hasCache {
		prev = store.ByProvider(cached)
	}

	cards := quota.Collect(ctx, providers, prev)
	now := time.Now()
	snap := store.Snapshot{UpdatedAt: now, Cards: cards}
	// 缓存写入失败不阻塞主流程（只影响下次是否能免查询）。
	_ = store.Save(snap)

	return Result{Cards: applyView(cards, opts), UpdatedAt: now}, nil
}

// applyView 先按应用过滤，再按视图裁剪（默认每个应用只留一张）。
func applyView(cards []model.Card, opts Options) []model.Card {
	out := filterByApp(cards, opts.App)
	if opts.All {
		return out
	}
	return pickPrimaryPerApp(out)
}

// pickPrimaryPerApp 每个应用只保留一张：优先「使用中」，否则取排序后的第一张。
// filterByApp 已保证组内 current 在前，因此取首个即可。
func pickPrimaryPerApp(cards []model.Card) []model.Card {
	seen := make(map[string]bool, len(cards))
	out := make([]model.Card, 0, len(cards))
	for _, c := range cards {
		if seen[c.App] {
			continue
		}
		seen[c.App] = true
		out = append(out, c)
	}
	return out
}

func filterByApp(cards []model.Card, app string) []model.Card {
	out := make([]model.Card, 0, len(cards))
	for _, c := range cards {
		if app == "" || c.App == app {
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].App != out[j].App {
			return out[i].App < out[j].App
		}
		if out[i].Current != out[j].Current {
			return out[i].Current
		}
		return out[i].Name < out[j].Name
	})
	return out
}
