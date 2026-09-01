package ui

// 诊断用 VEH：捕获未被处理的原生异常，把异常码/出错地址/所在模块写进日志。
// 仅在设置 EXE_LAUNCHER_VEH=1 时启用，正式构建默认关闭。

import (
	"exe-launcher/internal/win32"
	"log"
	"os"
	"strings"
	"syscall"
	"unsafe"
)

var (
	procAddVectoredExceptionHandler = win32.Kernel32.NewProc("AddVectoredExceptionHandler")
	procGetModuleHandleExW          = win32.Kernel32.NewProc("GetModuleHandleExW")
	procGetModuleFileNameW          = win32.Kernel32.NewProc("GetModuleFileNameW")
)

// x64 EXCEPTION_RECORD 头部字段布局（只需要前 5 个字段）
type exceptionRecordHead struct {
	exceptionCode    uint32
	exceptionFlags   uint32
	exceptionRecord  uintptr
	exceptionAddress uintptr
	numberParameters uint32
	_                uint32
}

func installDebugVEH() {
	if os.Getenv("EXE_LAUNCHER_VEH") == "" {
		return
	}
	cb := syscall.NewCallback(func(pointers uintptr) uintptr {
		// pointers 是 *EXCEPTION_POINTERS，第一个字段是 *EXCEPTION_RECORD
		rec := *(**exceptionRecordHead)(unsafe.Pointer(&pointers))
		code := rec.exceptionCode
		// 只记录真正的硬崩溃，避免把 Go 自身 nil-deref 的软异常刷进日志
		switch code {
		case 0xC0000005, 0xC0000409, 0xC00000FD, 0xC000041D:
			addr := rec.exceptionAddress
			log.Printf("VEH: 原生异常 0x%X @ 0x%X (%s)", code, addr, moduleOf(addr))
			if code == 0xC0000005 && rec.numberParameters >= 2 {
				info := (*[15]uintptr)(unsafe.Add(unsafe.Pointer(rec), 32))
				access := "read"
				if info[0] == 1 {
					access = "write"
				}
				log.Printf("VEH: %s 访问非法地址 0x%X", access, info[1])
			}
		}
		return 0 // EXCEPTION_CONTINUE_SEARCH
	})
	procAddVectoredExceptionHandler.Call(1, cb)
}

// moduleOf 反查出地址所在模块与偏移。
func moduleOf(addr uintptr) string {
	const (
		fromAddress     = 0x4
		unchangedRefcnt = 0x2
	)
	var hMod uintptr
	flag := uintptr(fromAddress | unchangedRefcnt)
	if r, _, _ := procGetModuleHandleExW.Call(flag, addr, uintptr(unsafe.Pointer(&hMod))); r == 0 || hMod == 0 {
		return "未知模块"
	}
	var buf [1024]uint16
	n, _, _ := procGetModuleFileNameW.Call(hMod, uintptr(unsafe.Pointer(&buf[0])), 1024)
	name := syscall.UTF16ToString(buf[:n])
	// 取文件名部分，太长的路径截掉
	if i := strings.LastIndexByte(name, '\\'); i >= 0 {
		name = name[i+1:]
	}
	return name + "+0x" + hex(addr-hMod)
}

func hex(v uintptr) string {
	if v == 0 {
		return "0"
	}
	var digits []byte
	for v > 0 {
		d := byte(v & 0xF)
		if d < 10 {
			digits = append([]byte{d + '0'}, digits...)
		} else {
			digits = append([]byte{d - 10 + 'A'}, digits...)
		}
		v >>= 4
	}
	return string(digits)
}
