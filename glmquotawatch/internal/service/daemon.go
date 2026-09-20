package service

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"glmquotawatch/internal/quota"
	"glmquotawatch/internal/store"
)

// sampleTimeout 单次采样预算；sampleInterval 默认采样间隔（配置缺失/非法时兜底）。
const (
	sampleTimeout = 30 * time.Second
	defInterval   = 5 * time.Minute
)

// RunDaemon 常驻监控：单实例锁 → 先采一轮 → 按 interval 周期采样。
// 新触发档位时经 Notifier 发送告警（同轮多窗口合并为一条多行通知）；
// 采样失败只记日志保持运行；interval 配置变更在每轮开头热加载；
// ctx 取消（Ctrl+C）后优雅退出。通知发送失败绝不中断循环。
func (s *Service) RunDaemon(ctx context.Context, n Notifier, log *slog.Logger) error {
	release, err := s.st.Lock()
	if err != nil {
		var le *store.LockError
		if errors.As(err, &le) {
			return errf("locked", "%v", le)
		}
		return errf("internal", "获取单实例锁失败: %v", err)
	}
	defer release()

	interval := defInterval
	if cfg, cerr := s.st.LoadConfig(); cerr == nil {
		if d, perr := time.ParseDuration(cfg.Interval); perr == nil && d > 0 {
			interval = clampInterval(d)
		}
	}
	log.Info("监控已启动", "interval", interval.String(), "data_dir", s.st.Dir())

	sample := func() {
		sctx, cancel := context.WithTimeout(ctx, sampleTimeout)
		_, outs, err := s.SampleOnce(sctx)
		cancel()
		if err != nil {
			// ctx 已取消时不再刷屏
			if ctx.Err() != nil {
				return
			}
			log.Warn("采样失败（保持运行，下轮重试）", "err", err)
			return
		}
		var msgs []string
		for _, o := range outs {
			if o.Notify {
				msgs = append(msgs, o.Message)
			}
			if o.Cleared {
				log.Info("窗口用量回落，重新武装阈值", "key", o.Key)
			}
		}
		if len(msgs) == 0 {
			return
		}
		if nerr := n.Notify(quota.AlertTitle, strings.Join(msgs, "\n")); nerr != nil {
			log.Warn("toast 通知失败", "err", nerr)
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
			// 热加载 interval：手改 config.json 下一轮生效
			if cfg, cerr := s.st.LoadConfig(); cerr == nil {
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
