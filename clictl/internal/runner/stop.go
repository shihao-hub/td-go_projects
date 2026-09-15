// stop.go 停止后台实例：对该工具全部存活 PID 执行树杀并闭环记录。
package runner

import (
	"os/exec"
	"strconv"
	"time"

	"clictl/internal/store"
)

// KilledProcess 一个被成功终止的后台实例
type KilledProcess struct {
	LaunchID int64 `json:"launch_id"`
	PID      int   `json:"pid"`
}

// StopResult stop 命令的 JSON 输出形状
type StopResult struct {
	Name           string          `json:"name"`
	Killed         []KilledProcess `json:"killed"`          // 已确认终止（含进程树）
	Failed         []KilledProcess `json:"failed"`          // 杀后复探仍存活（taskkill 失败或权限不足）
	AlreadyStopped bool            `json:"already_stopped"` // 无存活实例（killed/failed 均空）
}

// Stop 全杀：终止该工具全部存活的后台实例。
// 树杀用 taskkill /PID <pid> /T /F——/T 连同子进程一起终止（托盘/服务程序常有子进程），
// /F 强杀（后台进程无控制台，无法优雅响应 Ctrl+C 类信号）。
// 强杀拿不到真实退出码，闭环记录统一回写 exit_code=1（TerminateProcess 惯例值）。
func Stop(st *store.Store, name string) (StopResult, *RunError) {
	tool, rerr := lookup(st, name)
	if rerr != nil {
		return StopResult{}, rerr
	}

	unfinished, err := st.UnfinishedStarts(tool.ID)
	if err != nil {
		return StopResult{}, &RunError{Code: "db_error", Message: "查询启动记录失败: " + err.Error(), ExitCode: 1}
	}
	alive := FilterAlive(unfinished, tool.Path)

	res := StopResult{Name: tool.Name, Killed: []KilledProcess{}, Failed: []KilledProcess{}}
	now := time.Now()
	for _, l := range alive {
		kp := KilledProcess{LaunchID: l.ID, PID: *l.PID}
		// 输出全部丢弃：成败以复探为准，不信 taskkill 的返回码（部分失败时它也返回非 0）
		_ = exec.Command("taskkill", "/PID", strconv.Itoa(*l.PID), "/T", "/F").Run()
		if Alive(*l.PID, tool.Path) {
			res.Failed = append(res.Failed, kp)
			continue
		}
		res.Killed = append(res.Killed, kp)
		// 闭环：duration = 存活时长（截至本次探测），exit_code=1（强杀约定值）
		startedAt, perr := time.Parse(store.TimeFmt, l.StartedAt)
		durMs := int64(0)
		if perr == nil {
			durMs = now.Sub(startedAt).Milliseconds()
		}
		_ = st.FinishLaunch(l.ID, durMs, 1)
	}
	res.AlreadyStopped = len(res.Killed) == 0 && len(res.Failed) == 0
	return res, nil
}
