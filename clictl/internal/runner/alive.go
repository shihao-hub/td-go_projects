// alive.go PID 探活：判断某次后台启动的进程是否仍在运行。
// 三重校验：OpenProcess 存在性 + GetExitCodeProcess 终止态（STILL_ACTIVE）+
// 可执行文件路径比对（防 PID 回收复用误判）。
package runner

import (
	"strings"
	"syscall"
	"unsafe"

	"clictl/internal/store"

	"golang.org/x/sys/windows"
)

// stillActive GetExitCodeProcess 的"尚未退出"哨兵值（Win32 惯例，非真退出码）
const stillActive = 259

var (
	kernel32                      = windows.NewLazySystemDLL("kernel32.dll")
	procQueryFullProcessImageName = kernel32.NewProc("QueryFullProcessImageNameW")
	procGetExitCodeProcess        = kernel32.NewProc("GetExitCodeProcess")
)

// exePath 取进程可执行文件的 Win32 完整路径（与注册的 tool.Path 同一形式）
func exePath(h windows.Handle) (string, bool) {
	buf := make([]uint16, windows.MAX_PATH)
	size := uint32(len(buf))
	r1, _, _ := procQueryFullProcessImageName.Call(
		uintptr(h), 0,
		uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)),
	)
	if r1 == 0 {
		return "", false
	}
	return strings.ToLower(windows.UTF16ToString(buf[:size])), true
}

// Alive 探活：进程存在、未退出、且（若给定 expectPath）exe 路径一致才判定存活。
// expectPath 为空时仅做存在 + 未退出检查。
// 关键点：进程退出后若仍有外部句柄引用（conhost/杀软等），进程对象并不销毁，
// OpenProcess 依旧成功——必须再查终止态，否则已退出进程会被误判为存活。
// 已知局限：SysWOW64/System32 重定向等路径变体会被误判为已退出；退出码恰为
// 259 的进程会被误判为存活（概率极低，均接受）
func Alive(pid int, expectPath string) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		// 只认 ERROR_ACCESS_DENIED 为"存在但权限不足"（仅存在性检查时判活）；
		// 其余（ERROR_INVALID_PARAMETER 等）一律 PID 不存在，判死
		if errno, ok := err.(syscall.Errno); ok && errno == windows.ERROR_ACCESS_DENIED {
			return expectPath == "" // 带路径时无法校验，保守判死（宁漏报不误报）
		}
		return false
	}
	defer windows.CloseHandle(h)

	var exitCode uint32
	r1, _, _ := procGetExitCodeProcess.Call(uintptr(h), uintptr(unsafe.Pointer(&exitCode)))
	if r1 == 0 || exitCode != stillActive {
		return false // 查询失败或已退出
	}

	if expectPath == "" {
		return true
	}
	actual, ok := exePath(h)
	return ok && actual == strings.ToLower(expectPath)
}

// FilterAlive 过滤出真正存活的未闭环启动记录（含其 PID）
func FilterAlive(launches []store.Launch, toolPath string) []store.Launch {
	alive := make([]store.Launch, 0, len(launches))
	for _, l := range launches {
		if l.PID != nil && Alive(*l.PID, toolPath) {
			alive = append(alive, l)
		}
	}
	return alive
}
