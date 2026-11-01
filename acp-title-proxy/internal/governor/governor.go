package governor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"language_projects/go_projects/acp-title-proxy/internal/pool"
)

// SessionState 表示会话状态。
type SessionState string

const (
	StateIdle SessionState = "idle"
	StateBusy SessionState = "busy"
)

// SessionInfo 记录单个受管会话的元数据。
type SessionInfo struct {
	PID          int          `json:"pid"`
	SessionID    string       `json:"session_id,omitempty"`
	State        SessionState `json:"state"`
	CreatedAt    time.Time    `json:"created_at"`
	LastActiveAt time.Time    `json:"last_active_at"`
}

// crossProcessLock 跨进程排他锁接口。
type crossProcessLock interface {
	Lock() error
	Unlock()
}

// Governor 管理并发 AGY 会话配额，超限时无感淘汰最旧空闲进程，都在运行时拦截。
type Governor struct {
	storePath    string
	maxSlots     int
	myPID        int
	lock         crossProcessLock
	isAliveFunc  func(int) bool
	killTreeFunc func(int) error

	mu sync.Mutex
}

func defaultStorePath() string {
	appData := os.Getenv("APPDATA")
	if appData != "" {
		return filepath.Join(appData, "language_projects", "acp-title-proxy", "managed_sessions.json")
	}
	home, err := os.UserHomeDir()
	if err == nil {
		return filepath.Join(home, ".language_projects", "acp-title-proxy", "managed_sessions.json")
	}
	return filepath.Join(".", "managed_sessions.json")
}

// NewGovernor 创建默认配置的 Governor。
func NewGovernor(maxSlots int) *Governor {
	if maxSlots <= 0 {
		maxSlots = 3
	}
	return &Governor{
		storePath:    defaultStorePath(),
		maxSlots:     maxSlots,
		myPID:        os.Getpid(),
		lock:         defaultProcessLock(),
		isAliveFunc:  defaultIsProcessAlive,
		killTreeFunc: pool.KillProcessTree,
	}
}

// NewGovernorWithConfig 支持依赖注入，供单元测试使用。
func NewGovernorWithConfig(
	maxSlots int,
	storePath string,
	lock crossProcessLock,
	isAlive func(int) bool,
	killTree func(int) error,
) *Governor {
	if maxSlots <= 0 {
		maxSlots = 3
	}
	return &Governor{
		storePath:    storePath,
		maxSlots:     maxSlots,
		myPID:        os.Getpid(),
		lock:         lock,
		isAliveFunc:  isAlive,
		killTreeFunc: killTree,
	}
}

// Acquire 竞争会话配额：
// 1. 清理已死亡进程与自身历史残留；
// 2. 若当前会话数达到上限，自动强杀淘汰最久未活跃的空闲会话 (StateIdle)；
// 3. 若所有会话均在执行任务 (StateBusy)，返回错误拒绝准入；
// 4. 将自身登记为 StateIdle。
func (g *Governor) Acquire() error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if err := g.lock.Lock(); err != nil {
		return fmt.Errorf("governor lock failed: %w", err)
	}
	defer g.lock.Unlock()

	sessions := g.loadLocked()

	// 1. 过滤已死亡进程与自身
	alive := make([]SessionInfo, 0, len(sessions))
	for _, s := range sessions {
		if s.PID != g.myPID && g.isAliveFunc(s.PID) {
			alive = append(alive, s)
		}
	}

	// 2. 超限判断与淘汰
	if len(alive) >= g.maxSlots {
		var oldestIdleIdx = -1
		var oldestTime time.Time

		for i, s := range alive {
			if s.State == StateIdle {
				if oldestIdleIdx == -1 || s.LastActiveAt.Before(oldestTime) {
					oldestIdleIdx = i
					oldestTime = s.LastActiveAt
				}
			}
		}

		if oldestIdleIdx == -1 {
			return fmt.Errorf("活跃会话数已达上限 (%d/%d)，且所有会话均在执行任务，无法自动淘汰", len(alive), g.maxSlots)
		}

		victim := alive[oldestIdleIdx]
		fmt.Fprintf(os.Stderr, "[acp-title-proxy] 活跃会话达上限 (%d)，自动淘汰最旧空闲会话 (PID: %d, 空闲起止: %s)\n",
			g.maxSlots, victim.PID, victim.LastActiveAt.Format("15:04:05"))

		if g.killTreeFunc != nil {
			_ = g.killTreeFunc(victim.PID)
		}

		// 从存活列表移除被淘汰的会话
		alive = append(alive[:oldestIdleIdx], alive[oldestIdleIdx+1:]...)
	}

	// 3. 登记当前进程
	now := time.Now()
	alive = append(alive, SessionInfo{
		PID:          g.myPID,
		State:        StateIdle,
		CreatedAt:    now,
		LastActiveAt: now,
	})

	return g.saveLocked(alive)
}

// Release 注销自身，退出时清理。
func (g *Governor) Release() {
	g.mu.Lock()
	defer g.mu.Unlock()

	if err := g.lock.Lock(); err != nil {
		return
	}
	defer g.lock.Unlock()

	sessions := g.loadLocked()
	remaining := make([]SessionInfo, 0, len(sessions))
	for _, s := range sessions {
		if s.PID != g.myPID && g.isAliveFunc(s.PID) {
			remaining = append(remaining, s)
		}
	}
	_ = g.saveLocked(remaining)
}

// OnPromptStart 标记当前会话为 busy。
func (g *Governor) OnPromptStart() {
	g.updateSelf(func(s *SessionInfo) {
		s.State = StateBusy
		s.LastActiveAt = time.Now()
	})
}

// OnPromptDone 标记当前会话为 idle。
func (g *Governor) OnPromptDone() {
	g.updateSelf(func(s *SessionInfo) {
		s.State = StateIdle
		s.LastActiveAt = time.Now()
	})
}

// OnSessionID 记录会话 SessionID。
func (g *Governor) OnSessionID(sid string) {
	if sid == "" {
		return
	}
	g.updateSelf(func(s *SessionInfo) {
		s.SessionID = sid
	})
}

// List 返回当前所有有效的受管会话。
func (g *Governor) List() []SessionInfo {
	g.mu.Lock()
	defer g.mu.Unlock()

	if err := g.lock.Lock(); err != nil {
		return nil
	}
	defer g.lock.Unlock()

	sessions := g.loadLocked()
	alive := make([]SessionInfo, 0, len(sessions))
	for _, s := range sessions {
		if g.isAliveFunc(s.PID) {
			alive = append(alive, s)
		}
	}
	if len(alive) != len(sessions) {
		_ = g.saveLocked(alive)
	}
	return alive
}

func (g *Governor) updateSelf(mutate func(s *SessionInfo)) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if err := g.lock.Lock(); err != nil {
		return
	}
	defer g.lock.Unlock()

	sessions := g.loadLocked()
	found := false
	for i := range sessions {
		if sessions[i].PID == g.myPID {
			mutate(&sessions[i])
			found = true
			break
		}
	}
	if !found {
		// 容错：若之前由于异常未登记，补充登记
		now := time.Now()
		item := SessionInfo{
			PID:          g.myPID,
			State:        StateIdle,
			CreatedAt:    now,
			LastActiveAt: now,
		}
		mutate(&item)
		sessions = append(sessions, item)
	}
	_ = g.saveLocked(sessions)
}

func (g *Governor) loadLocked() []SessionInfo {
	data, err := os.ReadFile(g.storePath)
	if err != nil {
		return nil
	}
	var list []SessionInfo
	if err := json.Unmarshal(data, &list); err != nil {
		return nil
	}
	return list
}

func (g *Governor) saveLocked(list []SessionInfo) error {
	dir := filepath.Dir(g.storePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(g.storePath, data, 0644)
}
