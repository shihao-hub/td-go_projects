//go:build windows

package pool

import (
	"strings"
	"time"

	"golang.org/x/sys/windows"
)

func getProcessTimes(pid int) (createdAt time.Time, totalCPUTime time.Duration, err error) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return time.Time{}, 0, err
	}
	defer windows.CloseHandle(h)

	var createTime, exitTime, kernelTime, userTime windows.Filetime
	if err := windows.GetProcessTimes(h, &createTime, &exitTime, &kernelTime, &userTime); err != nil {
		return time.Time{}, 0, err
	}

	createdAt = time.Unix(0, createTime.Nanoseconds())
	totalCPUTime = time.Duration(kernelTime.Nanoseconds() + userTime.Nanoseconds())
	return createdAt, totalCPUTime, nil
}

func (r *Reaper) isProcessCPUIdle(pid int, sampleDuration time.Duration) bool {
	_, cpu1, err := getProcessTimes(pid)
	if err != nil {
		return false
	}

	time.Sleep(sampleDuration)

	_, cpu2, err := getProcessTimes(pid)
	if err != nil {
		return false
	}

	// 若在采样周期内没有消耗任何 CPU 时间，说明处于纯阻塞/闲置等待
	return (cpu2 - cpu1) == 0
}

// treeCPU 返回 pid 及其全部后代（工具子进程、编译任务等）的累计 CPU 时间。
// 只统计 harness 自身会把「正在跑构建、正在执行命令」误判为空闲。
func treeCPU(pid int, parents map[int]int) time.Duration {
	children := make(map[int][]int, len(parents))
	for child, parent := range parents {
		children[parent] = append(children[parent], child)
	}

	var total time.Duration
	seen := make(map[int]bool)
	stack := []int{pid}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[cur] {
			continue
		}
		seen[cur] = true
		if _, cpu, err := getProcessTimes(cur); err == nil {
			total += cpu
		}
		stack = append(stack, children[cur]...)
	}
	return total
}

// collect 扫描并返回按「命中的 worker 祖先」分组的全部 harness，以及全量父子映射。
//
// 分组键取 worker 祖先而不是直接父进程：agy_acp_server.exe 是 PyInstaller
// onefile，引导壳会再派生一个自身的子进程充当真正的服务端，harness 的直接
// 父进程是后者。只有沿父链追溯才能把同一 worker 下的 harness 归到同一组，
// 否则 reaper 既扫不到（判定失配），也可能把同一 worker 拆成多组各留一份。
func (r *Reaper) collect() (map[int][]HarnessInfo, map[int]int) {
	agyProcs, parents, _, _ := scanProcesses()

	knownParentPids := make(map[int]bool)
	// 显式传入 worker PID 时只扫描这些 worker；未传入时才自动发现全部
	// agy_acp_server，避免共享 daemon 误杀预热池里的 harness。
	discoverAll := r.getWorkerPids == nil
	if discoverAll {
		for _, p := range agyProcs {
			if strings.EqualFold(p.name, "agy_acp_server.exe") {
				knownParentPids[p.pid] = true
			}
		}
	} else {
		for _, pid := range r.getWorkerPids() {
			if pid > 0 {
				knownParentPids[pid] = true
			}
		}
	}

	grouped := make(map[int][]HarnessInfo)

	for _, p := range agyProcs {
		if !strings.EqualFold(p.name, "localharness_external.exe") {
			continue
		}
		owner, ok := WorkerAncestor(p.pid, knownParentPids, parents)
		if !ok {
			continue
		}
		created, _, err := getProcessTimes(p.pid)
		if err != nil {
			continue
		}
		grouped[owner] = append(grouped[owner], HarnessInfo{
			PID:       p.pid,
			ParentPID: p.parent,
			CreatedAt: created,
		})
	}

	return grouped, parents
}
