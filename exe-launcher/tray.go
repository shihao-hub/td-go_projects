package main

import (
	"log"
	"syscall"
	"unsafe"
)

// 系统托盘：Shell_NotifyIconW + 主窗口 wndproc 回调消息。
// 回调用 legacy（未 NIM_SETVERSION）语义：lParam 即鼠标消息，wParam 为图标 ID。
// explorer 重启后凭 TaskbarCreated 广播重挂图标。

const (
	wmAppTray  = wmApp + 1 // 托盘回调消息
	trayIconID = 1

	idTrayShow = 2001 // 托盘菜单：打开主窗口
	idTrayQuit = 2002 // 托盘菜单：退出
)

var (
	trayReady         bool
	taskbarCreatedMsg uint32
	hTrayIcon         uintptr
)

// loadTrayIcon 从 exe 资源 #1 取小尺寸图标（托盘与窗口共用同一资源）。
func loadTrayIcon() uintptr {
	hInst, _, _ := pGetModuleHandleW.Call(0)
	cx, _, _ := pGetSystemMetrics.Call(smCxsmIcon)
	cy, _, _ := pGetSystemMetrics.Call(smCysmIcon)
	h, _, _ := pLoadImageW.Call(hInst, trayIconID, imageIcon, cx, cy, lrDefaultColor)
	return h
}

func trayNid(hwnd uintptr, tip string) notifyIconDataW {
	nid := notifyIconDataW{
		cbSize:           uint32(unsafe.Sizeof(notifyIconDataW{})),
		hWnd:             hwnd,
		uID:              trayIconID,
		uFlags:           nifMessage | nifIcon | nifTip,
		uCallbackMessage: wmAppTray,
		hIcon:            hTrayIcon,
	}
	copy(nid.szTip[:], utf16Encode(tip, len(nid.szTip)))
	return nid
}

// utf16Encode 把 s 编码进定长 uint16 缓冲（含 NUL，超出截断）。
func utf16Encode(s string, max int) []uint16 {
	u, err := syscall.UTF16FromString(s)
	if err != nil {
		u = []uint16{0}
	}
	if len(u) > max {
		u = u[:max-1]
		u = append(u, 0)
	}
	return u
}

// trayInit 注册托盘图标；失败不致命（仅少了托盘入口）。
func trayInit(hwnd uintptr) bool {
	if hTrayIcon == 0 {
		hTrayIcon = loadTrayIcon()
	}
	if hTrayIcon == 0 {
		log.Printf("托盘图标加载失败，托盘不可用")
		return false
	}
	nid := trayNid(hwnd, mainTitle)
	r, _, err := pShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&nid)))
	if r == 0 {
		log.Printf("托盘图标注册失败: %v", err)
		return false
	}
	trayReady = true

	// 记录 TaskbarCreated 广播消息 ID（explorer 重启 → 重挂图标）
	if m, _, _ := pRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(mustUTF16("TaskbarCreated")))); m != 0 {
		taskbarCreatedMsg = uint32(m)
	}
	log.Printf("托盘已注册 (icon=0x%X)", hTrayIcon)
	return true
}

// trayRemove 摘除托盘图标（退出/重挂前调用），幂等。
func trayRemove(hwnd uintptr) {
	if !trayReady {
		return
	}
	nid := trayNid(hwnd, "")
	pShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&nid)))
	trayReady = false
}

// trayReAdd explorer 重启后重挂。
func trayReAdd(hwnd uintptr) {
	if !trayReady {
		return
	}
	trayRemove(hwnd)
	trayReady = false
	nid := trayNid(hwnd, mainTitle)
	if h, _, _ := pShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&nid))); h != 0 {
		trayReady = true
		log.Printf("托盘图标已重挂 (TaskbarCreated)")
	}
}

// onTrayMessage 处理托盘回调：左键单击恢复窗口，右键弹菜单。
func onTrayMessage(w *mainWindow, lParam uintptr) {
	switch uint32(lParam) {
	case wmLbuttonUp, wmLbuttonDbl:
		w.showFromTray()
	case wmRbuttonUp:
		w.showTrayMenu()
	}
}

// showFromTray 恢复主窗口到前台。
func (w *mainWindow) showFromTray() {
	if vis, _, _ := pIsWindowVisible.Call(w.hwnd); vis == 0 {
		if ic, _, _ := pIsIconic.Call(w.hwnd); ic != 0 {
			pShowWindow.Call(w.hwnd, swRestore)
		} else {
			pShowWindow.Call(w.hwnd, swShow)
		}
	}
	pSetForegroundWindow.Call(w.hwnd)
}

// showTrayMenu 托盘右键菜单：打开主窗口 / 退出。
// 与列表右键菜单同一套弹菜单流程（前置 SetForegroundWindow + 后置 WM_NULL）。
func (w *mainWindow) showTrayMenu() {
	menu, _, _ := pCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer pDestroyMenu.Call(menu)

	pAppendMenuW.Call(menu, mfString, idTrayShow, uintptr(unsafe.Pointer(mustUTF16("打开主窗口"))))
	pAppendMenuW.Call(menu, mfSeparator, 0, 0)
	pAppendMenuW.Call(menu, mfString, idTrayQuit, uintptr(unsafe.Pointer(mustUTF16("退出"))))

	var pt point
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	pSetForegroundWindow.Call(w.hwnd)
	pTrackPopupMenu.Call(menu, tpmRightButton,
		uintptr(uint32(pt.x)), uintptr(uint32(pt.y)), 0, w.hwnd, 0)
	pPostMessageW.Call(w.hwnd, wmNull, 0, 0)
}
