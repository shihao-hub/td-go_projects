//go:build windows

// conpty.go：基于 UserExistsError/conpty 的真 PTY 工厂实现。
package pty

import (
	"fmt"
	"sync"
	"unsafe"

	"github.com/UserExistsError/conpty"
	"golang.org/x/sys/windows"
)

// ConPtyFactory 真 ConPTY 工厂。
type ConPtyFactory struct{}

// Start 启动一个带伪控制台的进程，并绑入 Job Object 以便整树终止。
func (ConPtyFactory) Start(opts Options) (Term, error) {
	if !conpty.IsConPtyAvailable() {
		return nil, fmt.Errorf("当前 Windows 版本不支持 ConPTY")
	}
	cols, rows := opts.Cols, opts.Rows
	if cols <= 0 {
		cols = 120
	}
	if rows <= 0 {
		rows = 30
	}
	args := []conpty.ConPtyOption{conpty.ConPtyDimensions(cols, rows)}
	if opts.WorkDir != "" {
		args = append(args, conpty.ConPtyWorkDir(opts.WorkDir))
	}
	if len(opts.Env) > 0 {
		args = append(args, conpty.ConPtyEnv(opts.Env))
	}
	c, err := conpty.Start(opts.CommandLine, args...)
	if err != nil {
		return nil, fmt.Errorf("启动终端进程失败: %w", err)
	}
	term := &conPtyTerm{c: c}
	term.assignJob()
	return term, nil
}

// conPtyTerm 适配 *conpty.ConPty 到 Term 接口，并持有 Job Object。
type conPtyTerm struct {
	c   *conpty.ConPty
	mu  sync.Mutex
	job windows.Handle
}

func (t *conPtyTerm) Read(p []byte) (int, error)  { return t.c.Read(p) }
func (t *conPtyTerm) Write(p []byte) (int, error) { return t.c.Write(p) }
func (t *conPtyTerm) Resize(cols, rows int) error { return t.c.Resize(cols, rows) }
func (t *conPtyTerm) Pid() int                    { return t.c.Pid() }

// assignJob 把 shell 进程加入带 KILL_ON_JOB_CLOSE 的 Job Object。
func (t *conPtyTerm) assignJob() {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	); err != nil {
		_ = windows.CloseHandle(job)
		return
	}
	proc, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE|windows.PROCESS_QUERY_LIMITED_INFORMATION,
		false,
		uint32(t.Pid()),
	)
	if err != nil {
		_ = windows.CloseHandle(job)
		return
	}
	defer windows.CloseHandle(proc)
	if err := windows.AssignProcessToJobObject(job, proc); err != nil {
		_ = windows.CloseHandle(job)
		return
	}
	t.job = job
}

// MemoryBytes 读取 shell 进程当前工作集。
func (t *conPtyTerm) MemoryBytes() uint64 {
	proc, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(t.Pid()))
	if err != nil {
		return 0
	}
	defer windows.CloseHandle(proc)
	var counters processMemoryCounters
	counters.Cb = uint32(unsafe.Sizeof(counters))
	if err := getProcessMemoryInfo(proc, &counters, counters.Cb); err != nil {
		return 0
	}
	return uint64(counters.WorkingSetSize)
}

// Close 先用 Job Object 整树终止，再关闭 ConPTY。
func (t *conPtyTerm) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.job != 0 {
		_ = windows.TerminateJobObject(t.job, 0)
		_ = windows.CloseHandle(t.job)
		t.job = 0
	}
	return t.c.Close()
}

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

var getProcessMemoryInfoProc = windows.NewLazySystemDLL("psapi.dll").NewProc("GetProcessMemoryInfo")

func getProcessMemoryInfo(process windows.Handle, counters *processMemoryCounters, cb uint32) error {
	r1, _, err := getProcessMemoryInfoProc.Call(
		uintptr(process),
		uintptr(unsafe.Pointer(counters)),
		uintptr(cb),
	)
	if r1 == 0 {
		return err
	}
	return nil
}
