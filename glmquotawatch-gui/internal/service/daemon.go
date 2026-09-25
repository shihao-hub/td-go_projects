package service

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"glmquotawatch-gui/internal/quota"
)

// sampleTimeout 单次采样预算；defInterval 默认采样间隔（配置缺失/非法时兜底）。
const (
	sampleTimeout = 30 * time.Second
	defInterval   = 5 * time.Minute
)

// DaemonHooks daemon 运行期回调（两个字段均 nil 安全），GUI 壳用来驱动
// 托盘 tooltip、前端事件与错误横幅；CLI 无 daemon 场景，传零值即可。
type DaemonHooks struct {
	OnSample func(StatusView) // 每轮采样成功后收到最新视图
	OnError  func(error)      // 每轮采样失败时收到业务错误（含 service.Error）
}

// RunDaemon 常驻监控：先采一轮 → 按 interval 周期采样。
// 新触发档位时经 Notifier 发送告警（同轮多窗口合并为一条多行通知）；
// 采样失败只记日志保持运行（并回调 hooks.OnError）；interval 配置变更在
// 每轮开头热加载（fixedInterval 非零时跳过热加载直用固定值，演示模式用）；
// ctx 取消（托盘退出）后优雅退出。通知发送失败绝不中断循环。
func (s *Service) RunDaemon(ctx context.Context, n Notifier, log *slog.Logger, hooks DaemonHooks) error {
	interval := s.fixedInterval
	silent := false
	if interval == 0 {
		if cfg, cerr := s.st.LoadConfig(); cerr == nil {
			if d, perr := time.ParseDuration(cfg.Interval); perr == nil && d > 0 {
				interval = clampInterval(d)
			} else {
				interval = defInterval
			}
			silent = cfg.Silent
		} else {
			interval = defInterval
		}
	}
	log.Info("监控已启动", "interval", interval.String(), "data_dir", s.st.Dir())

	sample := func() {
		sctx, cancel := context.WithTimeout(ctx, sampleTimeout)
		view, outs, err := s.SampleOnce(sctx)
		cancel()
		if err != nil {
			// ctx 已取消时不再刷屏
			if ctx.Err() != nil {
				return
			}
			log.Warn("采样失败（保持运行，下轮重试）", "err", err)
			if hooks.OnError != nil {
				hooks.OnError(err)
			}
			return
		}
		if hooks.OnSample != nil {
			hooks.OnSample(view)
		}
		var msgs []string
		for _, o := range outs {
			if o.Notify {
				msgs = append(msgs, o.Message)
			}
			if o.Cleared {
				log.Info("窗口用量回落，重置告警记录", "key", o.Key)
			}
		}
		if len(msgs) == 0 {
			return
		}
		if nerr := n.Notify(quota.AlertTitle, strings.Join(msgs, "\n"), silent); nerr != nil {
			log.Warn("通知发送失败", "err", nerr)
		} else {
			log.Info("通知已发送", "windows", len(msgs))
		}
	}

	sample()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Info("收到退出信号，监控停止")
			return nil
		case <-ticker.C:
			if s.fixedInterval != 0 {
				// 演示模式：间隔与静音固定，跳过热加载
				sample()
				continue
			}
			// 热加载 interval 与 silent：手改 config.json / GUI 设置下一轮生效
			if cfg, cerr := s.st.LoadConfig(); cerr == nil {
				silent = cfg.Silent
				if d, perr := time.ParseDuration(cfg.Interval); perr == nil && d > 0 {
					if nd := clampInterval(d); nd != interval {
						interval = nd
						ticker.Reset(nd)
						log.Info("采样间隔已更新", "interval", nd.String())
					}
				}
			}
			sample()
		}
	}
}
