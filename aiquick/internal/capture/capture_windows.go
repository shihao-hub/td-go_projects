//go:build windows

// Package capture 实现「划词预填」：在唤起窗口前，对当前前台应用模拟一次
// Ctrl+C 抓取选中文本，并尽力还原原剪贴板纯文本内容。
// 局限（设计如此）：剪贴板原为图片/文件时无法还原，将以划选文本覆盖。
package capture

import (
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32               = windows.NewLazySystemDLL("user32.dll")
	kernel32             = windows.NewLazySystemDLL("kernel32.dll")
	procOpenClipboard    = user32.NewProc("OpenClipboard")
	procCloseClipboard   = user32.NewProc("CloseClipboard")
	procEmptyClipboard   = user32.NewProc("EmptyClipboard")
	procGetClipboardData = user32.NewProc("GetClipboardData")
	procSetClipboardData = user32.NewProc("SetClipboardData")
	procIsFormatAvail    = user32.NewProc("IsClipboardFormatAvailable")
	procSendInput        = user32.NewProc("SendInput")
	procLstrlenW         = kernel32.NewProc("lstrlenW")
	procGlobalAlloc      = kernel32.NewProc("GlobalAlloc")
	procGlobalLock       = kernel32.NewProc("GlobalLock")
	procGlobalUnlock     = kernel32.NewProc("GlobalUnlock")
)

const (
	cfUnicodeText = 13
	gmemMoveable  = 0x0002

	inputKeyboard = 1
	keyeventfKeyUp = 0x0002

	vkControl = 0x11
	vkMenu    = 0x12 // Alt
	vkShift   = 0x10
	vkLWin    = 0x5B
	vkKeyC    = 0x43
)

type keybdInput struct {
	wVk         uint16
	wScan       uint16
	dwFlags     uint32
	time        uint32
	dwExtraInfo uintptr
}

type mouseInput struct {
	dx, dy       int32
	mouseData    uint32
	dwFlags      uint32
	time         uint32
	dwExtraInfo uintptr
}

// input 布局需匹配 Win64 INPUT（40 字节，union 取最大成员 MOUSEINPUT）。
type input struct {
	typ uint32
	_   uint32
	mi  mouseInput
	_   [8]byte
}

func makeKeyInput(vk uint16, up bool) input {
	ki := keybdInput{wVk: vk}
	if up {
		ki.dwFlags = keyeventfKeyUp
	}
	in := input{typ: inputKeyboard}
	// 将 KEYBDINPUT 写入 union 区域
	dst := (*keybdInput)(unsafe.Pointer(&in.mi))
	*dst = ki
	return in
}

func sendInputs(ins ...input) {
	if len(ins) == 0 {
		return
	}
	procSendInput.Call(uintptr(len(ins)), uintptr(unsafe.Pointer(&ins[0])), unsafe.Sizeof(ins[0]))
}

var clipMu sync.Mutex

// sysPtr 把 syscall 返回的 uintptr 转为 unsafe.Pointer。
// vet 的 unsafeptr 检查只认可“内联在调用表达式里”的转换；
// 这里指向的是 GlobalLock 返回的非 Go 内存（剪贴板全局内存），
// 不存在 GC 移动/回收风险，双指针转换是安全的通行做法。
func sysPtr(v uintptr) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&v))
}

// readClipboardText 读取剪贴板纯文本（无文本时 ok=false）。
func readClipboardText() (string, bool) {
	ret, _, _ := syscall.Syscall(procOpenClipboard.Addr(), 1, 0, 0, 0)
	if ret == 0 {
		return "", false
	}
	defer syscall.Syscall(procCloseClipboard.Addr(), 0, 0, 0, 0)

	avail, _, _ := syscall.Syscall(procIsFormatAvail.Addr(), 1, cfUnicodeText, 0, 0)
	if avail == 0 {
		return "", false
	}
	h, _, _ := syscall.Syscall(procGetClipboardData.Addr(), 1, cfUnicodeText, 0, 0)
	if h == 0 {
		return "", false
	}
	p, _, _ := syscall.Syscall(procGlobalLock.Addr(), 1, h, 0, 0)
	if p == 0 {
		return "", false
	}
	defer syscall.Syscall(procGlobalUnlock.Addr(), 1, h, 0, 0)

	n, _, _ := syscall.Syscall(procLstrlenW.Addr(), 1, p, 0, 0)
	if n == 0 {
		return "", true
	}
	u16 := unsafe.Slice((*uint16)(sysPtr(p)), n)
	return string(utf16.Decode(u16)), true
}

// writeClipboardText 把纯文本写回剪贴板（尽力而为）。
func writeClipboardText(s string) bool {
	ret, _, _ := syscall.Syscall(procOpenClipboard.Addr(), 1, 0, 0, 0)
	if ret == 0 {
		return false
	}
	defer syscall.Syscall(procCloseClipboard.Addr(), 0, 0, 0, 0)

	if r, _, _ := syscall.Syscall(procEmptyClipboard.Addr(), 0, 0, 0, 0); r == 0 {
		return false
	}
	u16 := utf16.Encode([]rune(s + "\x00"))
	size := uintptr(len(u16)) * 2
	h, _, _ := syscall.Syscall(procGlobalAlloc.Addr(), 2, gmemMoveable, size, 0)
	if h == 0 {
		return false
	}
	p, _, _ := syscall.Syscall(procGlobalLock.Addr(), 1, h, 0, 0)
	if p == 0 {
		return false
	}
	copy(unsafe.Slice((*uint16)(sysPtr(p)), len(u16)), u16)
	syscall.Syscall(procGlobalUnlock.Addr(), 1, h, 0, 0)
	r, _, _ := syscall.Syscall(procSetClipboardData.Addr(), 2, cfUnicodeText, h, 0)
	return r != 0
}

// SelectedText 抓取前台窗口选中文本。
// 实现：记录原剪贴板文本 → 松开修饰键 → 模拟 Ctrl+C → 轮询剪贴板变化 → 还原。
// 抓取失败（无选区/目标程序不响应复制）返回 ("", false)，不报错弹窗。
func SelectedText(maxLen int) (string, bool) {
	clipMu.Lock()
	defer clipMu.Unlock()

	saved, hadText := readClipboardText()

	// 热键触发时修饰键通常还按着（如 Alt+S），先模拟松开，避免变成 Ctrl+Alt+C
	sendInputs(
		makeKeyInput(vkControl, true),
		makeKeyInput(vkMenu, true),
		makeKeyInput(vkShift, true),
		makeKeyInput(vkLWin, true),
	)
	time.Sleep(30 * time.Millisecond)
	sendInputs(
		makeKeyInput(vkControl, false),
		makeKeyInput(vkKeyC, false),
		makeKeyInput(vkKeyC, true),
		makeKeyInput(vkControl, true),
	)

	var captured string
	ok := false
	for i := 0; i < 12; i++ { // 最多等 300ms
		time.Sleep(25 * time.Millisecond)
		txt, has := readClipboardText()
		if has && txt != "" && txt != saved {
			captured, ok = txt, true
			break
		}
	}

	// 还原原文本（仅纯文本可还原；原文为空则保留划选结果）
	if ok && hadText && saved != captured {
		time.Sleep(30 * time.Millisecond)
		writeClipboardText(saved)
	}

	if !ok || captured == "" {
		return "", false
	}
	if maxLen > 0 && len([]rune(captured)) > maxLen {
		captured = string([]rune(captured)[:maxLen])
	}
	return strings.TrimRight(captured, "\r\n"), true
}
