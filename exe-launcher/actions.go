package main

import (
	"log"
	"os/exec"
	"path/filepath"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// launchExe 用 ShellExecute open 启动（与资源管理器双击一致，能处理需要 UAC 提权的 exe），
// lpDirectory 设为 exe 所在目录；失败再回落 CreateProcess。
func launchExe(path string) error {
	dir := filepath.Dir(path)
	log.Printf("启动 exe: %q (工作目录 %q)", path, dir)
	verb, _ := windows.UTF16PtrFromString("open")
	file, _ := windows.UTF16PtrFromString(path)
	cwd, _ := windows.UTF16PtrFromString(dir)
	if err := windows.ShellExecute(0, verb, file, nil, cwd, windows.SW_SHOWNORMAL); err == nil {
		log.Printf("启动 exe 成功 (ShellExecute)")
		return nil
	} else {
		log.Printf("ShellExecute 失败，回落 CreateProcess: %v", err)
	}
	cmd := exec.Command(path)
	cmd.Dir = dir
	return cmd.Start()
}

// revealInExplorer 打开父目录并高亮该文件。
func revealInExplorer(path string) error {
	log.Printf("打开目录: explorer /select %q", path)
	return exec.Command("explorer.exe", "/select,"+path).Start()
}

// openPowerShell 在 dir 打开新的 PowerShell 窗口；优先 pwsh，找不到回落 powershell.exe。
//
// 主路径必须用 ShellExecuteW 而不是 os/exec：exec 在 Stdin=nil 时会把子进程 stdin
// 接到 NUL 设备（立即 EOF），这会覆盖 CREATE_NEW_CONSOLE 本该给的键盘输入——
// 交互式 shell（powershell -NoExit / cmd /k）读到 EOF 就直接退出，
// 表现为窗口弹出打个横符便闪退。ShellExecuteW 不接 std 句柄，行为与资源管理器一致。
func openPowerShell(dir string) error {
	shell := "powershell.exe"
	if _, err := exec.LookPath("pwsh"); err == nil {
		shell = "pwsh"
	}
	log.Printf("打开 PowerShell: shell=%q dir=%q", shell, dir)

	verb, _ := windows.UTF16PtrFromString("open")
	file, _ := windows.UTF16PtrFromString(shell)
	params, _ := windows.UTF16PtrFromString("-NoExit")
	cwd, _ := windows.UTF16PtrFromString(dir)
	if err := windows.ShellExecute(0, verb, file, params, cwd, windows.SW_SHOWNORMAL); err == nil {
		log.Printf("PowerShell 已启动 (ShellExecute)")
		return nil
	} else {
		log.Printf("ShellExecute 失败，回落 CreateProcess: %v", err)
	}

	// 回落：裸 CreateProcess，不继承句柄、不接 std，让子进程用新控制台原生输入。
	si := syscall.StartupInfo{}
	si.Cb = uint32(unsafe.Sizeof(si))
	var pi syscall.ProcessInformation
	argv, _ := syscall.UTF16PtrFromString(shell)
	if err := syscall.CreateProcess(argv, nil, nil, nil, false, windows.CREATE_NEW_CONSOLE, nil, cwd, &si, &pi); err != nil {
		log.Printf("打开 PowerShell 失败: %v", err)
		return err
	}
	log.Printf("PowerShell 已启动 (CreateProcess, pid=%d)", pi.ProcessId)
	syscall.CloseHandle(pi.Process)
	syscall.CloseHandle(pi.Thread)
	return nil
}
