package pool

import (
	"fmt"
	"os"
	"time"

	"language_projects/go_projects/acp-title-proxy/internal/config"
)

// HarnessInfo 记录扫描到的 localharness 进程元数据。
type HarnessInfo struct {
	PID       int
	ParentPID int
	CreatedAt time.Time
}

// Reaper 负责后台巡检与闲置 localharness 进程回收。
type Reaper struct {
	cfg           config.ReaperConfig
	getWorkerPids func() []int
}

// NewReaper 创建一个闲置回收器。
func NewReaper(cfg config.ReaperConfig, getWorkerPids func() []int) *Reaper {
	if cfg.IdleMinutes <= 0 {
		cfg.IdleMinutes = 20
	}
	if cfg.CheckIntervalMinutes <= 0 {
		cfg.CheckIntervalMinutes = 5
	}
	return &Reaper{
		cfg:           cfg,
		getWorkerPids: getWorkerPids,
	}
}

// Start 启动后台定时巡检循环。
func (r *Reaper) Start(stopCh <-chan struct{}) {
	if !r.cfg.Enabled {
		return
	}

	interval := time.Duration(r.cfg.CheckIntervalMinutes) * time.Minute
	if interval < 10*time.Second {
		interval = 10 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// 启动后延时 30 秒执行首次巡检，避开启动时的初始化峰值
	select {
	case <-stopCh:
		return
	case <-time.After(30 * time.Second):
		r.RunOnce()
	}

	for {
		select {
		case <-stopCh:
			return
		case <-ticker.C:
			r.RunOnce()
		}
	}
}

// RunOnce 执行单次回收巡检。
func (r *Reaper) RunOnce() int {
	candidates := r.findCandidates()
	if len(candidates) == 0 {
		return 0
	}

	reapedCount := 0
	for _, h := range candidates {
		// 校验 CPU 闲置增量（采样 2 秒）
		if !r.isProcessCPUIdle(h.PID, 2*time.Second) {
			continue
		}

		fmt.Printf("[Reaper] Reaping idle AGY harness PID %d (Parent: %d, Created: %s)\n",
			h.PID, h.ParentPID, h.CreatedAt.Format("15:04:05"))
		if err := KillProcessTree(h.PID); err != nil {
			fmt.Fprintf(os.Stderr, "[Reaper] Failed to kill harness PID %d: %v\n", h.PID, err)
		} else {
			reapedCount++
		}
	}

	if reapedCount > 0 {
		fmt.Printf("[Reaper] Successfully reaped %d idle harness process(es)\n", reapedCount)
	}
	return reapedCount
}

// FilterCandidatesPure 纯函数逻辑，供单元测试验证：
// 给定某个 Parent 下的全部 harness 列表和超时时间，返回应当被回收的候选人列表（排除最新 1 个，且必须存活超时）。
func FilterCandidatesPure(harnesses []HarnessInfo, idleTimeout time.Duration, now time.Time) []HarnessInfo {
	if len(harnesses) <= 1 {
		// 最新且唯一的 1 个实例（当前活跃实例）永远豁免
		return nil
	}

	// 浅拷贝以防修改入参
	sorted := make([]HarnessInfo, len(harnesses))
	copy(sorted, harnesses)

	// 按创建时间升序排列：最旧的在前，最新的在末尾
	for i := 0; i < len(sorted)-1; i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[i].CreatedAt.After(sorted[j].CreatedAt) {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	// 豁免最后一个（最新活跃的实例）
	olderHarnesses := sorted[:len(sorted)-1]

	var candidates []HarnessInfo
	for _, h := range olderHarnesses {
		if now.Sub(h.CreatedAt) >= idleTimeout {
			candidates = append(candidates, h)
		}
	}
	return candidates
}
