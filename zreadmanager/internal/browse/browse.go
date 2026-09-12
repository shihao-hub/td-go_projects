// Package browse 管理 zread browse 子进程的生命周期（启动/停止/重启/状态）。
// CLI 每次调用都是独立进程，无法持有旧 cmd 对象，
// 因此用 pidfile（UserConfigDir/zreadmanager/running.json）记录活实例信息，
// stop/status 跨进程通过 PID + 进程创建时间探活定位实例（防 PID 复用误杀）。
package browse

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Options 启动参数（restart 时从 pidfile 复现）
type Options struct {
	Dir      string `json:"dir"`
	Host     string `json:"host,omitempty"`
	Port     int    `json:"port,omitempty"`
	Generate bool   `json:"generate,omitempty"`
}

// Running 活实例信息（pidfile 内容）
type Running struct {
	Options
	Pid        int    `json:"pid"`
	StartedAt  string `json:"started_at"`  // RFC3339
	CreateUnix int64  `json:"create_unix"` // 进程创建时间（unix 秒），探活时比对防 PID 复用
}

// 领域错误：cli 层单点映射为 JSON 错误码
var (
	// ErrZreadNotFound 本机未安装 zread 命令
	ErrZreadNotFound = errors.New("未找到 zread 命令，请先执行 npm install -g zread 安装")
	// ErrNoLastOptions restart 时无可复用的启动参数
	ErrNoLastOptions = errors.New("没有上次启动参数，restart 前请先用 start 指定 --dir")
)

// AlreadyRunningError start 时已有活实例
type AlreadyRunningError struct{ Cur Running }

func (e *AlreadyRunningError) Error() string {
	return fmt.Sprintf("已有活实例 pid=%d dir=%s，先 stop 或 restart", e.Cur.Pid, e.Cur.Dir)
}

// pidfilePath pidfile 位置：UserConfigDir/zreadmanager/running.json
func pidfilePath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "zreadmanager", "running.json"), nil
}

func loadRunning() (Running, bool) {
	p, err := pidfilePath()
	if err != nil {
		return Running{}, false
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return Running{}, false
	}
	var r Running
	if json.Unmarshal(data, &r) != nil || r.Pid <= 0 {
		return Running{}, false
	}
	return r, true
}

func saveRunning(r Running) error {
	p, err := pidfilePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o644)
}

func clearRunning() {
	if p, err := pidfilePath(); err == nil {
		_ = os.Remove(p)
	}
}

// Start 后台分离启动 zread browse：DETACHED 点火即走，CLI 立即返回。
// 已有活实例时返回 AlreadyRunningError（zreadmanager 是唯一管理者，不允许多实例）。
func Start(opt Options) (Running, error) {
	zreadCmd, err := lookZread()
	if err != nil {
		return Running{}, ErrZreadNotFound
	}
	if r, ok := loadRunning(); ok && alive(r.Pid, r.CreateUnix) {
		return Running{}, &AlreadyRunningError{Cur: r}
	}

	pid, err := spawnDetached(zreadCmd, opt)
	if err != nil {
		return Running{}, fmt.Errorf("启动 zread browse 失败: %w", err)
	}

	// 启动后立刻读创建时间，供后续跨进程探活比对
	createUnix, _ := processCreateUnix(pid)
	r := Running{
		Options:    opt,
		Pid:        pid,
		StartedAt:  time.Now().Format(time.RFC3339),
		CreateUnix: createUnix,
	}
	if err := saveRunning(r); err != nil {
		// pidfile 写失败则回滚：杀掉刚启动的进程，避免孤儿实例
		killTree(pid)
		return Running{}, fmt.Errorf("写入运行状态文件失败: %w", err)
	}
	return r, nil
}

// Stop 树杀活实例并清 pidfile。幂等语义：无活实例不报错。
// 返回（旧实例信息, 是否真的执行了停止）。
func Stop() (Running, bool, error) {
	r, ok := loadRunning()
	if !ok {
		return Running{}, false, nil
	}
	if !alive(r.Pid, r.CreateUnix) {
		// 记录中的进程已死（可能被外部杀掉），只清残留 pidfile
		clearRunning()
		return r, false, nil
	}
	if err := killTree(r.Pid); err != nil {
		return r, false, fmt.Errorf("停止 zread 失败: %w", err)
	}
	// 复探确认（最多约 2s）
	for i := 0; i < 20 && alive(r.Pid, r.CreateUnix); i++ {
		time.Sleep(100 * time.Millisecond)
	}
	clearRunning()
	return r, true, nil
}

// Restart 用上次参数重启（pidfile 优先，其次仅清状态后要求重新 start）。
func Restart() (Running, error) {
	r, ok := loadRunning()
	if !ok {
		return Running{}, ErrNoLastOptions
	}
	if alive(r.Pid, r.CreateUnix) {
		if _, _, err := Stop(); err != nil {
			return Running{}, err
		}
	}
	return Start(r.Options)
}

// Status 返回记录的实例信息与探活结果（只读，不改 pidfile）。
func Status() (Running, bool) {
	r, ok := loadRunning()
	if !ok {
		return Running{}, false
	}
	return r, alive(r.Pid, r.CreateUnix)
}
