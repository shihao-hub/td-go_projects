package core

import (
	"context"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"nutvault/internal/storage"
)

type SchedulerStatus struct {
	Running       bool      `json:"running"`
	IntervalMins  int       `json:"interval_mins"`
	LastRunAt     time.Time `json:"last_run_at,omitempty"`
	NextRunAt     time.Time `json:"next_run_at,omitempty"`
	TotalTargets  int       `json:"total_targets"`
	ActiveTargets int       `json:"active_targets"`
}

type Scheduler struct {
	engine *Engine
	store  *storage.Store

	mu           sync.Mutex
	running      bool
	intervalMins int
	lastRunAt    time.Time
	nextRunAt    time.Time
	cancelFn     context.CancelFunc
}

func NewScheduler(engine *Engine, store *storage.Store) *Scheduler {
	return &Scheduler{
		engine:       engine,
		store:        store,
		intervalMins: DefaultScheduleInterval,
	}
}

// Start 启动后台定时任务
func (s *Scheduler) Start() {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}

	// 从存储读取配置的周期
	intervalStr, err := s.store.GetConfig("global_schedule_interval_mins", strconv.Itoa(DefaultScheduleInterval))
	if err == nil {
		if val, err := strconv.Atoi(intervalStr); err == nil && val > 0 {
			s.intervalMins = val
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.cancelFn = cancel
	s.running = true
	s.nextRunAt = time.Now().Add(time.Duration(s.intervalMins) * time.Minute)
	s.mu.Unlock()

	go s.runLoop(ctx)
}

// Stop 停止调度器
func (s *Scheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return
	}
	if s.cancelFn != nil {
		s.cancelFn()
	}
	s.running = false
}

// GetStatus 获取当前调度状态
func (s *Scheduler) GetStatus() SchedulerStatus {
	s.mu.Lock()
	defer s.mu.Unlock()

	targets, _ := s.store.ListTargets()
	active := 0
	for _, t := range targets {
		if t.ScheduleEnabled {
			active++
		}
	}

	return SchedulerStatus{
		Running:       s.running,
		IntervalMins:  s.intervalMins,
		LastRunAt:     s.lastRunAt,
		NextRunAt:     s.nextRunAt,
		TotalTargets:  len(targets),
		ActiveTargets: active,
	}
}

// TriggerAll 立即执行一次全部计划备份
func (s *Scheduler) TriggerAll() []BackupResult {
	targets, err := s.store.ListTargets()
	if err != nil {
		slog.Error("调度器读取目标失败", "err", err)
		return nil
	}

	var results []BackupResult
	for _, t := range targets {
		if !t.ScheduleEnabled {
			continue
		}
		targetCopy := t
		res, err := s.engine.BackupTarget(&targetCopy, false)
		if err != nil && res == nil {
			results = append(results, BackupResult{
				TargetID:   targetCopy.ID,
				SourcePath: targetCopy.SourcePath,
				Status:     "failed",
				Message:    err.Error(),
			})
			continue
		}
		if res != nil {
			results = append(results, *res)
		}
	}

	s.mu.Lock()
	s.lastRunAt = time.Now()
	s.nextRunAt = s.lastRunAt.Add(time.Duration(s.intervalMins) * time.Minute)
	s.mu.Unlock()

	return results
}

func (s *Scheduler) runLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Duration(s.intervalMins) * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.TriggerAll()
		}
	}
}
