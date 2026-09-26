// Package service 是 agyquota 的公共业务核心：配额获取、解析归一化与
// 业务错误都归属于这里；只被 daemon 装配路径引用，CLI/MCP 不直接依赖。
// 本服务无状态、零落盘。
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"agyquota/internal/agapi"
	"agyquota/internal/api"

	"golang.org/x/sync/singleflight"
)

// Options 查询选项（领域入参）。
type Options struct {
	Source    string // "agy" 或 "zed"
	TokenFile string // 适用于 zed 模式的自定义凭据路径
}

// Fetcher 是取数接缝：默认走 agapi 真实数据源，
// 单元/集成测试可注入假数据源，不影响生产路径。
type Fetcher func(ctx context.Context, opt Options) (*agapi.UsageResult, string, error)

// Service 公共业务服务：无状态、可重入。
type Service struct {
	// Fetcher 可选；nil 时使用默认数据源（agy / zed）。
	Fetcher Fetcher

	// group 只包住 fetch：同源并发取数仅 leader 真正执行，
	// joined 请求共享结果；leader 的 progress 回调会触发，joined 无进度。
	group singleflight.Group
}

// New 创建服务。
func New() *Service {
	return &Service{}
}

// GetQuota 查询配额：根据 opt.Source 分发至 agy 或 zed 并归一化。
func (s *Service) GetQuota(ctx context.Context, opt Options, progress func(string)) (*api.Snapshot, error) {
	if opt.Source != api.SourceAgy && opt.Source != api.SourceZed {
		return nil, sourceRequired()
	}

	res, sourceLabel, err := s.fetch(ctx, opt, progress)
	if err != nil {
		return nil, err
	}

	snap, err := parseSnapshot(res.Raw, time.Now(), sourceLabel)
	if err != nil {
		return nil, api.Errorf(api.ErrParse, "解析配额响应失败: %v", err).
			WithSuggestion("可追加 --raw 参数查看底层接口的原始响应数据")
	}

	snap.Account = res.Account
	if res.ExpiresAt != nil {
		snap.TokenExpiresAt = res.ExpiresAt.Format("2006-01-02 15:04:05")
		d := time.Until(*res.ExpiresAt)
		if d > 0 {
			mins := int(d.Minutes())
			if mins >= 60 {
				snap.TokenExpiresIn = fmt.Sprintf("%d小时%d分钟后自动续期", mins/60, mins%60)
			} else {
				snap.TokenExpiresIn = fmt.Sprintf("%d分钟后自动续期", mins)
			}
		} else {
			snap.TokenExpiresIn = "待自动续期"
		}
	}

	return snap, nil
}

// FetchRaw 返回配额接口原始响应（--raw 调试用）。
func (s *Service) FetchRaw(ctx context.Context, opt Options, progress func(string)) (json.RawMessage, error) {
	if opt.Source != api.SourceAgy && opt.Source != api.SourceZed {
		return nil, sourceRequired()
	}

	res, _, err := s.fetch(ctx, opt, progress)
	if err != nil {
		return nil, err
	}
	return res.Raw, nil
}

// fetch 经 singleflight 合并同源并发取数，key = source + "|" + tokenFile。
// 注意：joined 请求共享 leader 的 ctx，leader 取消会连带失败同组请求
// （本地单用户场景可接受，见 requirements FR-1 例外条款）。
func (s *Service) fetch(ctx context.Context, opt Options, progress func(string)) (*agapi.UsageResult, string, error) {
	key := opt.Source + "|" + opt.TokenFile
	v, err, _ := s.group.Do(key, func() (any, error) {
		res, label, err := s.fetchWithDefault(ctx, opt, progress)
		if err != nil {
			return nil, err
		}
		return fetchResult{res: res, label: label}, nil
	})
	if err != nil {
		return nil, "", err
	}
	r := v.(fetchResult)
	return r.res, r.label, nil
}

type fetchResult struct {
	res   *agapi.UsageResult
	label string
}

// fetchWithDefault 优先使用注入的 Fetcher（测试接缝），否则走真实数据源。
func (s *Service) fetchWithDefault(ctx context.Context, opt Options, progress func(string)) (*agapi.UsageResult, string, error) {
	if s.Fetcher != nil {
		return s.Fetcher(ctx, opt)
	}
	return s.fetchBySource(ctx, opt, progress)
}

func sourceRequired() *api.Error {
	return api.Errorf(api.ErrSourceRequired, "未指定查询数据源").
		WithSuggestion("请通过参数显式指定查询目标：--agy（终端/桌面端）或 --zed（Zed 账号）")
}

func (s *Service) fetchBySource(ctx context.Context, opt Options, progress func(string)) (*agapi.UsageResult, string, error) {
	logf := func(format string, args ...any) {
		if progress != nil {
			progress(fmt.Sprintf(format, args...))
		}
	}

	switch opt.Source {
	case api.SourceZed:
		if progress != nil {
			progress("正在通过 Zed (antigravity-acp) 凭据查询模型配额 ...")
		}
		zc := agapi.NewZedClient(opt.TokenFile)
		zc.Logf = logf
		res, err := zc.FetchUsageWithMeta(ctx)
		if err != nil {
			return nil, "", api.Errorf(api.ErrZedExecute, "获取 Zed 对应账号配额失败: %v", err).
				WithSuggestion("请确认 ~/.gemini/antigravity-acp/acp_token.json 是否存在且网络正常")
		}
		return res, "Zed (antigravity-acp)", nil

	case api.SourceAgy:
		if progress != nil {
			progress("正在通过 Antigravity CLI (agy) 查询模型配额 ...")
		}
		ac := agapi.NewClient()
		ac.Logf = logf
		res, err := ac.FetchUsageWithMeta(ctx)
		if err != nil {
			return nil, "", api.Errorf(api.ErrAgyExecute, "获取 agy 配额失败: %v", err).
				WithSuggestion("请确认 agy 命令可执行且网络/代理正常；若是配额接口瞬态失败，请稍后重试")
		}
		return res, "Antigravity CLI (agy)", nil

	default:
		return nil, "", api.Errorf(api.ErrSourceRequired, "未知的数据源: %s", opt.Source)
	}
}
