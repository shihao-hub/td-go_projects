package pool

import (
	"fmt"
	"os"
	"sort"
	"sync"
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
//
// 判定单位是「单个 harness 连同它的工具子进程树」，不是整条 worker：
// 一个 worker 里同时开着 5 个会话时，只要还有 1 个在跑，worker 级活跃门
// 就会永远挡住回收；按 harness 判定才能在「一边用 A、其余闲置」时回收 B..E。
type Reaper struct {
	cfg           config.ReaperConfig
	getWorkerPids func() []int

	mu       sync.Mutex
	lastCPU  map[int]time.Duration // pid -> 上次观测到的子树累计 CPU
	lastBusy map[int]time.Time     // pid -> 最近一次 CPU 增长的观测时刻
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
		lastCPU:       make(map[int]time.Duration),
		lastBusy:      make(map[int]time.Time),
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
//
// 每次都采样每个 harness 子树（自身 + 工具子进程）的累计 CPU，并记录
// 「最近一次有 CPU 增长」的时刻；连续 idle_minutes 没有增长才会被回收。
// 组内保留最近活跃的 1 个作为兜底，避免把当前会话的运行时一并清掉。
func (r *Reaper) RunOnce() int {
	grouped, parents := r.collect()
	now := time.Now()

	type item struct {
		h    HarnessInfo
		busy time.Time
	}

	alive := make(map[int]bool)
	groups := make([][]item, 0, len(grouped))
	for _, list := range grouped {
		items := make([]item, 0, len(list))
		for _, h := range list {
			alive[h.PID] = true
			cpu := treeCPU(h.PID, parents)
			items = append(items, item{h: h, busy: r.observe(h.PID, cpu, now)})
		}
		groups = append(groups, items)
	}
	r.prune(alive)

	idle := time.Duration(r.cfg.IdleMinutes) * time.Minute
	reapedCount := 0

	for _, items := range groups {
		if len(items) <= 1 {
			// 组内唯一实例永远豁免
			continue
		}

		// 最近活跃的排在前面，索引 0 保留不杀
		sort.SliceStable(items, func(i, j int) bool { return items[i].busy.After(items[j].busy) })

		for i := 1; i < len(items); i++ {
			h := items[i]
			if now.Sub(h.busy) < idle {
				continue
			}
			// 最终复核：真要在 2 秒内一动都不动才动手，防误判
			if !r.isProcessCPUIdle(h.h.PID, 2*time.Second) {
				continue
			}

			fmt.Printf("[Reaper] Reaping idle AGY harness PID %d (Parent: %d, idle since %s)\n",
				h.h.PID, h.h.ParentPID, h.busy.Format("15:04:05"))
			if err := KillProcessTree(h.h.PID); err != nil {
				fmt.Fprintf(os.Stderr, "[Reaper] Failed to kill harness PID %d: %v\n", h.h.PID, err)
			} else {
				reapedCount++
			}
		}
	}

	if reapedCount > 0 {
		fmt.Printf("[Reaper] Successfully reaped %d idle harness process(es)\n", reapedCount)
	}
	return reapedCount
}

// observe 记录本次 CPU 采样并返回该 harness 最近一次活跃的时刻。
//
// 首次见到的进程一律给满一个完整宽限期（busy=now），避免 daemon 刚启动/
// 刚接管 worker 时把历史会话误判为「早就闲置」。
func (r *Reaper) observe(pid int, cpu time.Duration, now time.Time) time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()

	prev, seen := r.lastCPU[pid]
	r.lastCPU[pid] = cpu

	if !seen {
		r.lastBusy[pid] = now
		return now
	}
	if cpu > prev {
		r.lastBusy[pid] = now
		return now
	}
	if busy, ok := r.lastBusy[pid]; ok {
		return busy
	}
	r.lastBusy[pid] = now
	return now
}

// prune 清理已消失进程的历史状态，避免 PID 复用后继承旧记录。
func (r *Reaper) prune(alive map[int]bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for pid := range r.lastCPU {
		if !alive[pid] {
			delete(r.lastCPU, pid)
			delete(r.lastBusy, pid)
		}
	}
}

// Collect 返回按 worker 祖先分组的全部 harness（不做闲置过滤），供诊断使用。
func (r *Reaper) Collect() map[int][]HarnessInfo {
	groups, _ := r.collect()
	return groups
}

// WorkerAncestor 沿父链向上追溯，返回 pid 命中的 worker 祖先 PID。
//
// 背景：agy_acp_server.exe 是 PyInstaller onefile，引导壳会再派生一个自身的
// 子进程充当真正的服务端，localharness 的直接父进程是后者而不是 daemon 记录的
// worker PID。若只用「直接父进程 == worker PID」判定，reaper 会永远扫不到任何
// harness。这里向上追溯，命中任一 worker 祖先即视为该 worker 的会话。
//
// depth 上限用于防御 PID 回收造成的异常父子链。
func WorkerAncestor(pid int, workers map[int]bool, parents map[int]int) (int, bool) {
	cur := pid
	for i := 0; i < 8; i++ {
		parent, ok := parents[cur]
		if !ok || parent == 0 {
			return 0, false
		}
		if workers[parent] {
			return parent, true
		}
		cur = parent
	}
	return 0, false
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
