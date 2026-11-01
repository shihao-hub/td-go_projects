//go:build windows

package pool

import (
	"fmt"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

type agyProcRaw struct {
	pid    int
	name   string
	parent int
}

// scanProcesses 抓取全量进程快照，返回 AGY 进程列表、PID→父PID 映射、PID→进程名映射与存活集合。
func scanProcesses() (agy []agyProcRaw, parents map[int]int, names map[int]string, alive map[int]bool) {
	parents = make(map[int]int)
	names = make(map[int]string)
	alive = make(map[int]bool)

	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, parents, names, alive
	}
	defer windows.CloseHandle(snap)

	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	if err := windows.Process32First(snap, &entry); err != nil {
		return nil, parents, names, alive
	}
	for {
		pid := int(entry.ProcessID)
		parent := int(entry.ParentProcessID)
		name := windows.UTF16ToString(entry.ExeFile[:])

		alive[pid] = true
		parents[pid] = parent
		names[pid] = name
		if IsAGYProcess(name) {
			agy = append(agy, agyProcRaw{pid: pid, name: name, parent: parent})
		}

		if err := windows.Process32Next(snap, &entry); err != nil {
			break
		}
	}
	return agy, parents, names, alive
}

var (
	modpsapi                 = windows.NewLazySystemDLL("psapi.dll")
	procGetProcessMemoryInfo = modpsapi.NewProc("GetProcessMemoryInfo")
)

// processMemoryCounters 对应 Windows PROCESS_MEMORY_COUNTERS 结构体。
type processMemoryCounters struct {
	Cb                         uint32
	PageFaultCount             uint32
	PeakWorkingSetSize         uintptr
	WorkingSetSize             uintptr
	QuotaPeakPagedPoolUsage    uintptr
	QuotaPagedPoolUsage        uintptr
	QuotaPeakNonPagedPoolUsage uintptr
	QuotaNonPagedPoolUsage     uintptr
	PagefileUsage              uintptr
	PeakPagefileUsage          uintptr
}

func processMemoryMB(pid int) int {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return 0
	}
	defer windows.CloseHandle(h)

	var counters processMemoryCounters
	counters.Cb = uint32(unsafe.Sizeof(counters))
	r, _, _ := procGetProcessMemoryInfo.Call(uintptr(h), uintptr(unsafe.Pointer(&counters)), uintptr(counters.Cb))
	if r == 0 {
		return 0
	}
	return int(counters.WorkingSetSize / (1024 * 1024))
}

// isChainOrphan 判断该进程是否已成孤儿：
//   - 直接父进程已退出（PID 不存在）→ 孤儿；
//   - 父进程存活但不是 AGY 进程（如 acp-title-proxy 客户端/Zed）→ 不是孤儿；
//   - 父进程本身也是 AGY 进程时，递归判断父是否孤儿（父子一起泄漏的形态）。
func isChainOrphan(pid int, parents map[int]int, names map[int]string, alive map[int]bool, cache map[int]bool) bool {
	if v, ok := cache[pid]; ok {
		return v
	}
	// 先占位，防止异常父链成环
	cache[pid] = false

	parent, ok := parents[pid]
	if !ok || parent == 0 {
		return false
	}
	if !alive[parent] {
		cache[pid] = true
		return true
	}
	if !IsAGYProcess(names[parent]) {
		return false
	}
	result := isChainOrphan(parent, parents, names, alive, cache)
	cache[pid] = result
	return result
}

// ReapOrphans 扫描并清理父进程已退出的 AGY 进程（agy_acp_server 与 localharness_external）。
// dryRun 为 true 时只扫描不清理。父进程仍存活的进程一律不动，避免误杀活跃会话。
func ReapOrphans(dryRun bool) ReapResult {
	agy, parents, names, alive := scanProcesses()
	res := ReapResult{Scanned: len(agy), DryRun: dryRun}
	cache := make(map[int]bool)

	for _, p := range agy {
		if !isChainOrphan(p.pid, parents, names, alive, cache) {
			continue
		}
		mem := processMemoryMB(p.pid)
		res.FreedMB += mem
		created := ""
		if t, _, err := getProcessTimes(p.pid); err == nil {
			created = t.Format("15:04:05")
		}
		res.Orphans = append(res.Orphans, AGYProcess{
			PID:      p.pid,
			Name:     p.name,
			Parent:   p.parent,
			MemoryMB: mem,
			Created:  created,
		})
	}

	if dryRun {
		return res
	}

	for _, p := range res.Orphans {
		if err := KillProcessTree(p.PID); err != nil {
			res.Failures = append(res.Failures, fmt.Sprintf("pid %d: %v", p.PID, err))
			continue
		}
		res.Reaped++
	}

	// 等待进程真正退出后再复采一次，避免统计口径偏差
	time.Sleep(300 * time.Millisecond)
	return res
}
