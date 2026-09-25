package service

import (
	"context"
	"log/slog"
	"sort"
	"strings"
	"time"

	"glmquotawatch-gui/internal/quota"
	"glmquotawatch-gui/internal/store"
)

const (
	sampleTimeout = 30 * time.Second
	defInterval   = 5 * time.Minute
)

// DaemonHooks 是 daemon 运行期回调；所有字段 nil 安全。
type DaemonHooks struct {
	OnSample      func(StatusView) // 每轮采样成功后收到视图
	OnError       func(error)      // 每轮采样失败时收到业务错误
	OnNotifyError func(error)      // Toast 或告警状态写入失败时收到业务错误
}

// pendingAlert 保存发送失败后仍待确认的告警。
type pendingAlert struct {
	message string
	newly   []int
}

// RunDaemon 常驻监控：采样、评估阈值、发送告警，并在 Toast 确认成功后记账。
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

	pending := map[string]pendingAlert{}
	pendingThresholds := []int(nil)

	commitState := func(state store.State) error {
		if err := s.st.SaveState(state); err != nil {
			codedErr := errf("state_save_failed", "保存告警状态失败: %v", err)
			log.Warn("告警状态保存失败", "err", err)
			if hooks.OnNotifyError != nil {
				hooks.OnNotifyError(codedErr)
			}
			return err
		}
		return nil
	}

	sample := func() {
		sctx, cancel := context.WithTimeout(ctx, sampleTimeout)
		result, err := s.sample(sctx, false)
		cancel()
		if err != nil {
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
			hooks.OnSample(result.view(false))
		}

		if !equalIntSlice(pendingThresholds, result.next.Thresholds) {
			pending = map[string]pendingAlert{}
			pendingThresholds = append([]int(nil), result.next.Thresholds...)
		}
		currentKeys := map[string]bool{}
		for _, outcome := range result.outcomes {
			currentKeys[outcome.Key] = true
			if outcome.Notify {
				pending[outcome.Key] = pendingAlert{
					message: outcome.Message,
					newly:   append([]int(nil), outcome.Newly...),
				}
			}
			if outcome.Cleared {
				delete(pending, outcome.Key)
				log.Info("窗口用量回落，重置告警记录", "key", outcome.Key)
			}
		}
		for key := range pending {
			if !currentKeys[key] {
				delete(pending, key)
			}
		}

		if len(pending) == 0 {
			if err := commitState(result.next); err != nil {
				return
			}
			if hooks.OnSample != nil {
				hooks.OnSample(result.view(true))
			}
			return
		}

		keys := make([]string, 0, len(pending))
		for key := range pending {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		msgs := make([]string, 0, len(keys))
		for _, key := range keys {
			msgs = append(msgs, pending[key].message)
		}

		if nerr := n.Notify(quota.AlertTitle, strings.Join(msgs, "\n"), silent); nerr != nil {
			log.Warn("通知发送失败", "err", nerr)
			if hooks.OnNotifyError != nil {
				hooks.OnNotifyError(errf("notify_failed", "发送用量告警失败: %v", nerr))
			}
			return
		}
		log.Info("通知已发送", "windows", len(msgs))

		confirmed := result.next
		if confirmed.Notified == nil {
			confirmed.Notified = map[string][]int{}
		}
		for key, alert := range pending {
			confirmed.Notified[key] = mergeInts(confirmed.Notified[key], alert.newly)
		}
		if err := commitState(confirmed); err != nil {
			return
		}
		pending = map[string]pendingAlert{}
		if hooks.OnSample != nil {
			hooks.OnSample(result.view(true))
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
				sample()
				continue
			}
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

func equalIntSlice(a, b []int) bool {
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

func mergeInts(left, right []int) []int {
	out := append([]int(nil), left...)
	for _, value := range right {
		found := false
		for _, existing := range out {
			if existing == value {
				found = true
				break
			}
		}
		if !found {
			out = append(out, value)
		}
	}
	sort.Ints(out)
	return out
}
