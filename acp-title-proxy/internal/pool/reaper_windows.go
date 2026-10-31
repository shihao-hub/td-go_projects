//go:build windows

package pool

import (
	"strings"
	"time"
	"unsafe"

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

func (r *Reaper) findCandidates() []HarnessInfo {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snapshot)

	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))

	if err := windows.Process32First(snapshot, &entry); err != nil {
		return nil
	}

	knownParentPids := make(map[int]bool)
	if r.getWorkerPids != nil {
		for _, pid := range r.getWorkerPids() {
			if pid > 0 {
				knownParentPids[pid] = true
			}
		}
	}

	type rawProcess struct {
		pid       int
		parentPid int
		exeName   string
	}

	var allProcs []rawProcess
	for {
		exe := windows.UTF16ToString(entry.ExeFile[:])
		allProcs = append(allProcs, rawProcess{
			pid:       int(entry.ProcessID),
			parentPid: int(entry.ParentProcessID),
			exeName:   exe,
		})

		// 同时自动发现系统中运行的所有 agy_acp_server
		if strings.EqualFold(exe, "agy_acp_server.exe") {
			knownParentPids[int(entry.ProcessID)] = true
		}

		if err := windows.Process32Next(snapshot, &entry); err != nil {
			break
		}
	}

	// 按 ParentPID 对 localharness 分组
	grouped := make(map[int][]HarnessInfo)
	now := time.Now()

	for _, p := range allProcs {
		if strings.EqualFold(p.exeName, "localharness_external.exe") {
			if knownParentPids[p.parentPid] {
				created, _, err := getProcessTimes(p.pid)
				if err != nil {
					continue
				}
				grouped[p.parentPid] = append(grouped[p.parentPid], HarnessInfo{
					PID:       p.pid,
					ParentPID: p.parentPid,
					CreatedAt: created,
				})
			}
		}
	}

	idleTimeout := time.Duration(r.cfg.IdleMinutes) * time.Minute
	var allCandidates []HarnessInfo

	for _, list := range grouped {
		candidates := FilterCandidatesPure(list, idleTimeout, now)
		allCandidates = append(allCandidates, candidates...)
	}

	return allCandidates
}
