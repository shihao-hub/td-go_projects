//go:build windows

package agapi

import (
	"fmt"
	"io"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// jobObjectUILimitSetForeground 禁止作业内任何进程抢夺前台窗口；
// x/sys 未提供该常量，取值为 MSDN JOBOBJECT_BASIC_UI_RESTRICTIONS 的 UI 限制位
const jobObjectUILimitSetForeground = 0x00000100

// isolationDesktopName agy 子树专属私有桌面：其所有窗口/console 会话都诞生于此，
// 用户桌面的前台窗口不受任何影响
const isolationDesktopName = "agyquota_isolation_desktop"

var (
	modUser32          = windows.NewLazySystemDLL("user32.dll")
	procCreateDesktopW = modUser32.NewProc("CreateDesktopW")
	procCloseDesktop   = modUser32.NewProc("CloseDesktop")
)

// createIsolationDesktop 创建 agy 专属私有桌面（GENERIC_ALL）。
func createIsolationDesktop() (windows.Handle, error) {
	name16, err := windows.UTF16PtrFromString(isolationDesktopName)
	if err != nil {
		return 0, fmt.Errorf("编码私有桌面名失败: %w", err)
	}
	h, _, callErr := procCreateDesktopW.Call(
		uintptr(unsafe.Pointer(name16)),
		0, 0, 0,
		uintptr(windows.GENERIC_ALL),
		0,
	)
	if h == 0 {
		return 0, fmt.Errorf("创建私有桌面失败: %v", callErr)
	}
	return windows.Handle(h), nil
}

// agyProcess 是一次 agy 查询的进程句柄集合：
// 伪控制台让 agy 启动即拥有可用 console；私有桌面兜住它仍主动发起的 console/窗口请求
// （全部落在隐形桌面，用户桌面无感）；stdout/stderr 走独立管道保证 JSON 纯净；
// 作业对象双 UI 限制（前台+桌面）把整棵子树锁死。
type agyProcess struct {
	procH      windows.Handle
	jobH       windows.Handle
	pcH        windows.Handle
	desktopH   windows.Handle
	attrList   *windows.ProcThreadAttributeListContainer
	conptyInW  windows.Handle
	stdoutFile *os.File
	stderrFile *os.File
	conptyDump *os.File
}

// closeHandles 批量关闭句柄，忽略错误（收尾路径）。
func closeHandles(handles ...windows.Handle) {
	for _, h := range handles {
		if h != 0 {
			windows.CloseHandle(h)
		}
	}
}

// startAgyProcess 以「伪控制台 + 私有桌面 + 独立管道 + UI 限制作业」启动 agy：
// 挂起态创建、入作业、恢复线程，任一步失败即杀进程并逆序释放资源。
func startAgyProcess(exePath string) (*agyProcess, error) {
	// 0. 私有桌面（agy 子树的窗口/console 会话全部落在隐形桌面，用户桌面无感）
	desktopH, err := createIsolationDesktop()
	if err != nil {
		return nil, err
	}

	// 1. 伪控制台所需管道
	var conptyInR, conptyInW, conptyOutR, conptyOutW windows.Handle
	if err := windows.CreatePipe(&conptyInR, &conptyInW, nil, 0); err != nil {
		windows.CloseHandle(desktopH)
		return nil, fmt.Errorf("创建伪控制台输入管道失败: %w", err)
	}
	if err := windows.CreatePipe(&conptyOutR, &conptyOutW, nil, 0); err != nil {
		closeHandles(conptyInR, conptyInW, desktopH)
		return nil, fmt.Errorf("创建伪控制台输出管道失败: %w", err)
	}

	// 2. 伪控制台本体（headless 管道实现，不产生可见窗口、不经默认终端委托）
	var pc windows.Handle
	if err := windows.CreatePseudoConsole(windows.Coord{X: 80, Y: 25}, conptyInR, conptyOutW, 0, &pc); err != nil {
		closeHandles(conptyInR, conptyInW, conptyOutR, conptyOutW, desktopH)
		return nil, fmt.Errorf("创建伪控制台失败: %w", err)
	}
	// 会话已交付伪控制台，父进程侧立即释放两端
	windows.CloseHandle(conptyInR)
	windows.CloseHandle(conptyOutW)

	// 3. agy 输出管道（可继承），原始 JSON 绕开伪控制台渲染
	sa := &windows.SecurityAttributes{InheritHandle: 1}
	sa.Length = uint32(unsafe.Sizeof(*sa))
	var stdoutR, stdoutW, stderrR, stderrW windows.Handle
	if err := windows.CreatePipe(&stdoutR, &stdoutW, sa, 0); err != nil {
		closeHandles(conptyInW, conptyOutR, desktopH)
		windows.ClosePseudoConsole(pc)
		return nil, fmt.Errorf("创建 stdout 管道失败: %w", err)
	}
	if err := windows.CreatePipe(&stderrR, &stderrW, sa, 0); err != nil {
		closeHandles(stdoutR, stdoutW, conptyInW, conptyOutR, desktopH)
		windows.ClosePseudoConsole(pc)
		return nil, fmt.Errorf("创建 stderr 管道失败: %w", err)
	}

	// 4. 作业对象双 UI 限制：禁抢前台 + 禁跨桌面（agy 被锁死在私有桌面内）
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		closeHandles(stdoutR, stdoutW, stderrR, stderrW, conptyInW, conptyOutR, desktopH)
		windows.ClosePseudoConsole(pc)
		return nil, fmt.Errorf("创建作业对象失败: %w", err)
	}
	var ui windows.JOBOBJECT_BASIC_UI_RESTRICTIONS
	ui.UIRestrictionsClass = jobObjectUILimitSetForeground | windows.JOB_OBJECT_UILIMIT_DESKTOP
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectBasicUIRestrictions,
		uintptr(unsafe.Pointer(&ui)), uint32(unsafe.Sizeof(ui))); err != nil {
		closeHandles(stdoutR, stdoutW, stderrR, stderrW, conptyInW, conptyOutR, job, desktopH)
		windows.ClosePseudoConsole(pc)
		return nil, fmt.Errorf("设置作业 UI 限制失败: %w", err)
	}

	// 5. 属性列表挂载伪控制台
	al, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		closeHandles(stdoutR, stdoutW, stderrR, stderrW, conptyInW, conptyOutR, job, desktopH)
		windows.ClosePseudoConsole(pc)
		return nil, fmt.Errorf("初始化进程属性列表失败: %w", err)
	}
	pcv := uintptr(pc)
	if err := al.Update(windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE, unsafe.Pointer(&pcv), unsafe.Sizeof(pcv)); err != nil {
		al.Delete()
		closeHandles(stdoutR, stdoutW, stderrR, stderrW, conptyInW, conptyOutR, job, desktopH)
		windows.ClosePseudoConsole(pc)
		return nil, fmt.Errorf("挂载伪控制台属性失败: %w", err)
	}

	// 6. 挂起态创建 agy：std 句柄指向独立管道，console 即伪控制台，桌面为私有桌面
	exe16, e := windows.UTF16PtrFromString(exePath)
	if e != nil {
		al.Delete()
		closeHandles(stdoutR, stdoutW, stderrR, stderrW, conptyInW, conptyOutR, job, desktopH)
		windows.ClosePseudoConsole(pc)
		return nil, fmt.Errorf("编码 agy 路径失败: %w", e)
	}
	cmdline := `"` + exePath + `" -p /usage --output-format json`
	cmd16, e := windows.UTF16PtrFromString(cmdline)
	if e != nil {
		al.Delete()
		closeHandles(stdoutR, stdoutW, stderrR, stderrW, conptyInW, conptyOutR, job, desktopH)
		windows.ClosePseudoConsole(pc)
		return nil, fmt.Errorf("编码命令行失败: %w", e)
	}
	desktop16, e := windows.UTF16PtrFromString(isolationDesktopName)
	if e != nil {
		al.Delete()
		closeHandles(stdoutR, stdoutW, stderrR, stderrW, conptyInW, conptyOutR, job, desktopH)
		windows.ClosePseudoConsole(pc)
		return nil, fmt.Errorf("编码桌面名失败: %w", e)
	}

	siEx := &windows.StartupInfoEx{}
	siEx.Cb = uint32(unsafe.Sizeof(*siEx))
	siEx.Flags = windows.STARTF_USESTDHANDLES
	siEx.Desktop = desktop16
	siEx.StdInput = conptyInW
	siEx.StdOutput = stdoutW
	siEx.StdErr = stderrW
	siEx.ProcThreadAttributeList = al.List()

	var pi windows.ProcessInformation
	if err := windows.CreateProcess(exe16, cmd16, nil, nil, true,
		windows.EXTENDED_STARTUPINFO_PRESENT|windows.CREATE_SUSPENDED, nil, nil, &siEx.StartupInfo, &pi); err != nil {
		al.Delete()
		closeHandles(stdoutR, stdoutW, stderrR, stderrW, conptyInW, conptyOutR, job, desktopH)
		windows.ClosePseudoConsole(pc)
		return nil, fmt.Errorf("启动 agy 进程失败: %w", err)
	}

	// 7. 入作业（UI 限制即刻生效）→ 恢复执行
	if err := windows.AssignProcessToJobObject(job, pi.Process); err != nil {
		_ = windows.TerminateProcess(pi.Process, 1)
		windows.CloseHandle(pi.Thread)
		windows.CloseHandle(pi.Process)
		al.Delete()
		closeHandles(stdoutR, stdoutW, stderrR, stderrW, conptyInW, conptyOutR, job, desktopH)
		windows.ClosePseudoConsole(pc)
		return nil, fmt.Errorf("agy 进程加入作业失败: %w", err)
	}
	windows.ResumeThread(pi.Thread)
	windows.CloseHandle(pi.Thread)

	// 8. 父进程侧释放写端（保证读端 EOF），包装读端；伪控制台输出持续排水防阻塞
	windows.CloseHandle(stdoutW)
	windows.CloseHandle(stderrW)

	p := &agyProcess{procH: pi.Process, jobH: job, pcH: pc, desktopH: desktopH, attrList: al, conptyInW: conptyInW}
	p.stdoutFile = os.NewFile(uintptr(stdoutR), "agy-stdout")
	p.stderrFile = os.NewFile(uintptr(stderrR), "agy-stderr")
	p.conptyDump = os.NewFile(uintptr(conptyOutR), "agy-conpty-dump")
	go func() { _, _ = io.Copy(io.Discard, p.conptyDump) }()
	return p, nil
}

// Stdout 返回 agy 原始标准输出（独立管道，未经伪控制台 VT 渲染）。
func (p *agyProcess) Stdout() io.Reader { return p.stdoutFile }

// Stderr 返回 agy 原始标准错误。
func (p *agyProcess) Stderr() io.Reader { return p.stderrFile }

// Wait 阻塞等待 agy 退出并返回退出码；超时时由 shutdown 终止整树解除阻塞。
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

// shutdown 终止整树并释放全部句柄；幂等，可安全 defer 与超时路径重复调用。
func (p *agyProcess) shutdown() {
	if p.jobH != 0 {
		_ = windows.TerminateJobObject(p.jobH, 1)
	}
	if p.pcH != 0 {
		windows.ClosePseudoConsole(p.pcH)
		p.pcH = 0
	}
	if p.stdoutFile != nil {
		_ = p.stdoutFile.Close()
		p.stdoutFile = nil
	}
	if p.stderrFile != nil {
		_ = p.stderrFile.Close()
		p.stderrFile = nil
	}
	if p.conptyDump != nil {
		_ = p.conptyDump.Close()
		p.conptyDump = nil
	}
	if p.procH != 0 {
		windows.CloseHandle(p.procH)
		p.procH = 0
	}
	if p.jobH != 0 {
		windows.CloseHandle(p.jobH)
		p.jobH = 0
	}
	if p.desktopH != 0 {
		procCloseDesktop.Call(uintptr(p.desktopH))
		p.desktopH = 0
	}
	if p.conptyInW != 0 {
		windows.CloseHandle(p.conptyInW)
		p.conptyInW = 0
	}
	if p.attrList != nil {
		p.attrList.Delete()
		p.attrList = nil
	}
}
