//go:build windows

package cli

import (
	"os"
	"syscall"
	"unsafe"
)

var (
	modkernel32        = syscall.NewLazyDLL("kernel32.dll")
	procAttachConsole   = modkernel32.NewProc("AttachConsole")
	procFreeConsole     = modkernel32.NewProc("FreeConsole")
	procWriteConsoleInp = modkernel32.NewProc("WriteConsoleInputW")
)

const attachParentProcess = ^uintptr(0)

const (
	keyEventRecord = 0x0001
	vkReturn       = 0x0D
)

type keyEvent struct {
	BKeyDown          int32
	WRepeatCount      uint16
	WVirtualKeyCode   uint16
	WVirtualScanCode  uint16
	UnicodeChar       uint16
	DwControlKeyState uint32
}

type inputRecord struct {
	EventType uint16
	_         uint16
	KeyEvent  keyEvent
}

var attachedToParent bool

// AttachParentConsole 在处于 CLI 模式时将 stdout/stderr 附着到父控制台（PowerShell / CMD）。
func AttachParentConsole() {
	r, _, _ := procAttachConsole.Call(attachParentProcess)
	if r != 0 {
		attachedToParent = true
		hout, err := syscall.GetStdHandle(syscall.STD_OUTPUT_HANDLE)
		if err == nil && hout != syscall.InvalidHandle {
			os.Stdout = os.NewFile(uintptr(hout), "/dev/stdout")
		}
		herr, err := syscall.GetStdHandle(syscall.STD_ERROR_HANDLE)
		if err == nil && herr != syscall.InvalidHandle {
			os.Stderr = os.NewFile(uintptr(herr), "/dev/stderr")
		}
	}
}

// DetachParentConsole 在命令执行完毕时清理控制台附着。
// 向父控制台输入缓冲区发送虚拟 Enter 键，唤醒并恢复 PowerShell / CMD 的提示符，彻底消除光标假性卡死现象。
func DetachParentConsole() {
	if !attachedToParent {
		return
	}
	if os.Stdout != nil {
		_ = os.Stdout.Sync()
	}
	if os.Stderr != nil {
		_ = os.Stderr.Sync()
	}

	hin, err := syscall.GetStdHandle(syscall.STD_INPUT_HANDLE)
	if err == nil && hin != syscall.InvalidHandle {
		records := []inputRecord{
			{
				EventType: keyEventRecord,
				KeyEvent: keyEvent{
					BKeyDown:        1,
					WRepeatCount:    1,
					WVirtualKeyCode: vkReturn,
					UnicodeChar:     vkReturn,
				},
			},
			{
				EventType: keyEventRecord,
				KeyEvent: keyEvent{
					BKeyDown:        0,
					WRepeatCount:    1,
					WVirtualKeyCode: vkReturn,
					UnicodeChar:     vkReturn,
				},
			},
		}
		var written uint32
		procWriteConsoleInp.Call(
			uintptr(hin),
			uintptr(unsafe.Pointer(&records[0])),
			uintptr(len(records)),
			uintptr(unsafe.Pointer(&written)),
		)
	}

	procFreeConsole.Call()
}