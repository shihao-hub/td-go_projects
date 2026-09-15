//go:build windows

package mcp

import (
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// procTree 用 Windows Job Object 管理本次执行的全部进程：
//   - setup：CREATE_SUSPENDED 挂起主线程启动，杜绝"启动到入 Job 之间
//     spawn 孙进程逃逸"的竞态窗口；
//   - attach：Start 后立刻把进程挂进 Job 再 resume 主线程；
//   - Job 带 KILL_ON_JOB_CLOSE：server 自身崩溃/被杀时，OS 关闭句柄
//     兜底回收整棵进程树，不留孤儿。
type procTree struct {
	job windows.Handle
}

func newProcTree() (*procTree, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	); err != nil {
		windows.CloseHandle(job)
		return nil, err
	}
	return &procTree{job: job}, nil
}

// setup 在 Start 前配置子进程属性（挂起启动）
func (p *procTree) setup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_SUSPENDED}
}

// attach 在 cmd.Start 成功后调用：进程入 Job + 恢复主线程
func (p *procTree) attach(cmd *exec.Cmd) error {
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	if err := windows.AssignProcessToJobObject(p.job, h); err != nil {
		return err
	}
	return resumeMainThread(cmd.Process.Pid)
}

// kill 终止整棵进程树（Termination 退出码用强杀约定值 1）
func (p *procTree) kill() {
	_ = windows.TerminateJobObject(p.job, 1)
}

// close 释放 Job 句柄（最后一个进程退出后 Job 自毁；若仍有存活进程，
// KILL_ON_JOB_CLOSE 保证整树回收）
func (p *procTree) close() {
	_ = windows.CloseHandle(p.job)
}

// resumeMainThread 用线程快照定位属主为 pid 的线程并恢复其执行。
// CREATE_SUSPENDED 只挂起主线程，快照里按属主 PID 匹配第一个即为目标。
func resumeMainThread(pid int) error {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(snap)
	var te windows.ThreadEntry32
	te.Size = uint32(unsafe.Sizeof(te))
	if err := windows.Thread32First(snap, &te); err != nil {
		return err
	}
	for {
		if int(te.OwnerProcessID) == pid {
			h, err := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, te.ThreadID)
			if err != nil {
				return err
			}
			defer windows.CloseHandle(h)
			_, err = windows.ResumeThread(h)
			return err
		}
		if err := windows.Thread32Next(snap, &te); err != nil {
			return err // 遍历完未找到属主线程（不应发生）
		}
	}
}
