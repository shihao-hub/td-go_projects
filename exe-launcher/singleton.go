package main

import (
	"log"
	"syscall"
	"unsafe"
)

// 单例：内核命名 Mutex 做跨进程权威标记。
// Local\ 前缀 = 会话级，多用户终端服务器互不干扰；进程退出由内核自动回收，无需释放。

const mutexName = `Local\exe-launcher-singleton`

var hMutex uintptr

// acquireSingleInstance 返回 true 表示成功持有（首实例）；
// false 表示已有实例在运行，此时顺带把第一实例窗口带到前台。
func acquireSingleInstance() bool {
	h, _, err := pCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(mustUTF16(mutexName))))
	hMutex = h
	if err == syscall.ERROR_ALREADY_EXISTS {
		log.Printf("已检测到运行中的实例 (mutex=%s)", mutexName)
		activateExistingWindow()
		return false
	}
	log.Printf("持有单例 Mutex (mutex=%s)", mutexName)
	return true
}

// activateExistingWindow 按窗口类名找到第一实例窗口，恢复可见（可能被隐藏到托盘
// 或最小化）并置前台。第二实例刚被用户启动、持有前台权限，可合法转移给第一实例。
func activateExistingWindow() {
	hwnd, _, _ := pFindWindowW.Call(uintptr(unsafe.Pointer(mustUTF16(mainClassName))), 0)
	if hwnd == 0 {
		log.Printf("未找到已有实例窗口 (class=%s)", mainClassName)
		return
	}
	if vis, _, _ := pIsWindowVisible.Call(hwnd); vis == 0 {
		// 隐藏到托盘的窗口：先恢复可见（最小化态走还原，否则直接显示）
		if ic, _, _ := pIsIconic.Call(hwnd); ic != 0 {
			pShowWindow.Call(hwnd, swRestore)
		} else {
			pShowWindow.Call(hwnd, swShow)
		}
	} else if ic, _, _ := pIsIconic.Call(hwnd); ic != 0 {
		pShowWindow.Call(hwnd, swRestore)
	}
	pSetForegroundWindow.Call(hwnd)
}
