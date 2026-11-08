package sysinfo

import (
	"fmt"
	"sort"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"

	"rebootremind/internal/model"
)

var (
	kernel32                 = windows.NewLazySystemDLL("kernel32.dll")
	psapi                    = windows.NewLazySystemDLL("psapi.dll")
	procGlobalMemoryStatusEx = kernel32.NewProc("GlobalMemoryStatusEx")
	procGetTickCount64       = kernel32.NewProc("GetTickCount64")
	procGetProcessMemoryInfo = psapi.NewProc("GetProcessMemoryInfo")
)

type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

type processMemoryCountersEx struct {
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
	PrivateUsage               uintptr
}

// Collect 采集开机时长、内存水位与 Top N 内存进程。
func Collect(topN int) (model.Snapshot, error) {
	tick, _, _ := procGetTickCount64.Call()
	uptime := int64(tick / 1000)

	memory, err := readMemory()
	if err != nil {
		return model.Snapshot{}, err
	}
	top, err := readTopProcesses(memory.TotalBytes, topN)
	if err != nil {
		return model.Snapshot{}, err
	}
	return model.Snapshot{
		UptimeSeconds: uptime,
		Memory:        memory,
		TopProcesses:  top,
	}, nil
}

func readMemory() (model.MemoryStatus, error) {
	var status memoryStatusEx
	status.Length = uint32(unsafe.Sizeof(status))
	ok, _, callErr := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&status)))
	if ok == 0 {
		return model.MemoryStatus{}, fmt.Errorf("GlobalMemoryStatusEx 失败: %w", callErr)
	}
	if status.TotalPhys == 0 {
		return model.MemoryStatus{}, fmt.Errorf("GlobalMemoryStatusEx 返回物理内存为 0")
	}
	used := status.TotalPhys - status.AvailPhys
	return model.MemoryStatus{
		TotalBytes: status.TotalPhys,
		UsedBytes:  used,
		Percent:    round1(float64(used) / float64(status.TotalPhys) * 100),
	}, nil
}

func readTopProcesses(totalBytes uint64, topN int) ([]model.ProcessUsage, error) {
	if topN <= 0 {
		topN = 5
	}
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, fmt.Errorf("枚举进程失败: %w", err)
	}
	defer windows.CloseHandle(snapshot)

	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	sums := make(map[string]uint64)
	names := make(map[string]string)
	for err = windows.Process32First(snapshot, &entry); err == nil; err = windows.Process32Next(snapshot, &entry) {
		if entry.ProcessID == 0 {
			continue
		}
		name := windows.UTF16ToString(entry.ExeFile[:])
		if name == "" || strings.EqualFold(name, "System Idle Process") {
			continue
		}
		bytes := processWorkingSet(entry.ProcessID)
		if bytes == 0 {
			continue
		}
		key := strings.ToLower(name)
		if _, seen := names[key]; !seen {
			names[key] = name
		}
		sums[key] += bytes
	}

	type pair struct {
		name  string
		bytes uint64
	}
	pairs := make([]pair, 0, len(sums))
	for key, bytes := range sums {
		pairs = append(pairs, pair{name: key, bytes: bytes})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].bytes == pairs[j].bytes {
			return pairs[i].name < pairs[j].name
		}
		return pairs[i].bytes > pairs[j].bytes
	})
	if len(pairs) > topN {
		pairs = pairs[:topN]
	}

	result := make([]model.ProcessUsage, 0, len(pairs))
	for _, p := range pairs {
		percent := 0.0
		if totalBytes > 0 {
			percent = round1(float64(p.bytes) / float64(totalBytes) * 100)
		}
		result = append(result, model.ProcessUsage{
			Name:            names[p.name],
			WorkingSetBytes: p.bytes,
			Percent:         percent,
		})
	}
	return result, nil
}

func processWorkingSet(pid uint32) uint64 {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.PROCESS_VM_READ, false, pid)
	if err != nil {
		handle, err = windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
		if err != nil {
			return 0
		}
	}
	defer windows.CloseHandle(handle)

	var counters processMemoryCountersEx
	counters.Cb = uint32(unsafe.Sizeof(counters))
	ok, _, _ := procGetProcessMemoryInfo.Call(
		uintptr(handle),
		uintptr(unsafe.Pointer(&counters)),
		uintptr(counters.Cb),
	)
	if ok == 0 {
		return 0
	}
	return uint64(counters.WorkingSetSize)
}

func round1(value float64) float64 {
	return float64(int64(value*10+0.5)) / 10
}
