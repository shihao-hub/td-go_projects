//go:build windows

package agapi

import (
	"fmt"
	"io"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// agyProcess 是一次 agy 查询的进程句柄集合：
// 采用原生控制台继承模式（不创建独立桌面与伪控制台），自然复用父进程的终端会话，
// 杜绝 Windows 11 DefTerm 误判脱钩而弹出新 Tab；
// stdout/stderr 走独立管道捕获 JSON；作业对象（Job Object）保证退出或超时时原子终止整树。
type agyProcess struct {
	procH      windows.Handle
	jobH       windows.Handle
	stdoutFile *os.File
	stderrFile *os.File
}

// closeHandles 批量关闭句柄，忽略错误（收尾路径）。
func closeHandles(handles ...windows.Handle) {
	for _, h := range handles {
		if h != 0 {
			windows.CloseHandle(h)
		}
	}
}

// startAgyProcess 以原生控制台继承 + 管道捕获 + 作业对象启动 agy：
// 挂起态创建、入作业、恢复线程，任一步失败即杀进程并逆序释放资源。
func startAgyProcess(exePath string) (*agyProcess, error) {
	sa := &windows.SecurityAttributes{InheritHandle: 1}
	sa.Length = uint32(unsafe.Sizeof(*sa))

	// 1. agy 输出管道（可继承），捕获原始 stdout 与 stderr
	var stdoutR, stdoutW, stderrR, stderrW windows.Handle
	if err := windows.CreatePipe(&stdoutR, &stdoutW, sa, 0); err != nil {
		return nil, fmt.Errorf("创建 stdout 管道失败: %w", err)
	}
	if err := windows.CreatePipe(&stderrR, &stderrW, sa, 0); err != nil {
		closeHandles(stdoutR, stdoutW)
		return nil, fmt.Errorf("创建 stderr 管道失败: %w", err)
	}

	// 2. 作业对象：确保 agyquota 退出或取消时，agy 及其产生的任何后代进程被原子收尾
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		closeHandles(stdoutR, stdoutW, stderrR, stderrW)
		return nil, fmt.Errorf("创建作业对象失败: %w", err)
	}
	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		closeHandles(stdoutR, stdoutW, stderrR, stderrW, job)
		return nil, fmt.Errorf("设置作业对象限制失败: %w", err)
	}

	// 3. 命令行与标准输入重定向（输入定向到 NUL，防交互等待）
	nul16, _ := windows.UTF16PtrFromString("NUL")
	nulH, err := windows.CreateFile(nul16, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, sa, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		closeHandles(stdoutR, stdoutW, stderrR, stderrW, job)
		return nil, fmt.Errorf("打开 NUL 失败: %w", err)
	}
	defer windows.CloseHandle(nulH)

	runPath, err := ensureSilentAgyExe(exePath)
	if err != nil {
		runPath = exePath
	}

	exe16, err := windows.UTF16PtrFromString(runPath)
	if err != nil {
		closeHandles(stdoutR, stdoutW, stderrR, stderrW, job)
		return nil, fmt.Errorf("编码 agy 路径失败: %w", err)
	}
	cmdline := `"` + runPath + `" -p /usage --output-format json`
	cmd16, err := windows.UTF16PtrFromString(cmdline)
	if err != nil {
		closeHandles(stdoutR, stdoutW, stderrR, stderrW, job)
		return nil, fmt.Errorf("编码命令行失败: %w", err)
	}

	si := &windows.StartupInfo{}
	si.Cb = uint32(unsafe.Sizeof(*si))
	si.Flags = windows.STARTF_USESTDHANDLES
	si.StdInput = nulH
	si.StdOutput = stdoutW
	si.StdErr = stderrW

	// 4. 挂起态创建：不传 DETACHED_PROCESS、不传 CREATE_NEW_CONSOLE，直接继承父会话
	var pi windows.ProcessInformation
	if err := windows.CreateProcess(exe16, cmd16, nil, nil, true,
		windows.CREATE_SUSPENDED, nil, nil, si, &pi); err != nil {
		closeHandles(stdoutR, stdoutW, stderrR, stderrW, job)
		return nil, fmt.Errorf("启动 agy 进程失败: %w", err)
	}

	// 5. 入作业 → 恢复执行
	if err := windows.AssignProcessToJobObject(job, pi.Process); err != nil {
		_ = windows.TerminateProcess(pi.Process, 1)
		windows.CloseHandle(pi.Thread)
		windows.CloseHandle(pi.Process)
		closeHandles(stdoutR, stdoutW, stderrR, stderrW, job)
		return nil, fmt.Errorf("agy 进程加入作业失败: %w", err)
	}
	windows.ResumeThread(pi.Thread)
	windows.CloseHandle(pi.Thread)

	// 6. 父进程侧释放写端，保证读端能感知 EOF
	windows.CloseHandle(stdoutW)
	windows.CloseHandle(stderrW)

	p := &agyProcess{
		procH:      pi.Process,
		jobH:       job,
		stdoutFile: os.NewFile(uintptr(stdoutR), "agy-stdout"),
		stderrFile: os.NewFile(uintptr(stderrR), "agy-stderr"),
	}
	return p, nil
}

// Stdout 返回 agy 原始标准输出。
func (p *agyProcess) Stdout() io.Reader { return p.stdoutFile }

// Stderr 返回 agy 原始标准错误。
func (p *agyProcess) Stderr() io.Reader { return p.stderrFile }

// Wait 阻塞等待 agy 退出并返回退出码。
func (p *agyProcess) Wait() (uint32, error) {
	if _, err := windows.WaitForSingleObject(p.procH, windows.INFINITE); err != nil {
		return 1, fmt.Errorf("等待 agy 进程退出失败: %w", err)
	}
	var code uint32
	if err := windows.GetExitCodeProcess(p.procH, &code); err != nil {
		return 1, fmt.Errorf("获取 agy 退出码失败: %w", err)
	}
	return code, nil
}

// shutdown 终止整树并释放句柄；幂等。
func (p *agyProcess) shutdown() {
	if p.jobH != 0 {
		_ = windows.TerminateJobObject(p.jobH, 1)
		windows.CloseHandle(p.jobH)
		p.jobH = 0
	}
	if p.stdoutFile != nil {
		_ = p.stdoutFile.Close()
		p.stdoutFile = nil
	}
	if p.stderrFile != nil {
		_ = p.stderrFile.Close()
		p.stderrFile = nil
	}
	if p.procH != 0 {
		windows.CloseHandle(p.procH)
		p.procH = 0
	}
}
