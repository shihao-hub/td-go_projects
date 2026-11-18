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
		return Result{Cards: filterByApp(cached.Cards, opts.App), UpdatedAt: cached.UpdatedAt, FromCache: true}, nil
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

	return Result{Cards: filterByApp(cards, opts.App), UpdatedAt: now}, nil
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
