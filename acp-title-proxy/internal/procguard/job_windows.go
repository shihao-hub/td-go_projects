//go:build windows

package procguard

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// Guard 持有一个 Windows Job Object（KILL_ON_JOB_CLOSE）。
// 关闭句柄或宿主进程退出时，内核会连带终止 Job 内的所有进程及其子进程。
type Guard struct {
	job windows.Handle
}

// Attach 把 pid 及其后续创建的子进程绑定到一个 Job Object。
// 失败时返回 error，调用方应降级为不启用守护，绝不能因此阻塞启动。
func Attach(pid int) (*Guard, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
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
		return nil, err
	}

	h, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE|windows.PROCESS_QUERY_LIMITED_INFORMATION,
		false,
		uint32(pid),
	)
	if err != nil {
		_ = windows.CloseHandle(job)
		return nil, err
	}
	defer windows.CloseHandle(h)

	if err := windows.AssignProcessToJobObject(job, h); err != nil {
		_ = windows.CloseHandle(job)
		return nil, err
	}

	return &Guard{job: job}, nil
}

// Close 关闭 Job 句柄；因为设置了 KILL_ON_JOB_CLOSE，Job 内的进程树会被内核一并终止。
func (g *Guard) Close() error {
	if g == nil || g.job == 0 {
		return nil
	}
	h := g.job
	g.job = 0
	return windows.CloseHandle(h)
}
