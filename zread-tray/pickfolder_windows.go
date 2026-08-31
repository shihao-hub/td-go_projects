//go:build windows

package main

import (
	"fmt"
	"log"
	"os"
	"runtime"
	"syscall"
	"time"
	"unsafe"
)

const (
	coinitApartmentThreaded = 0x2
	clsctxInprocServer      = 0x1

	fosPickFolders     = 0x20
	fosForceFilesystem = 0x40
	fosPathMustExist   = 0x800
	fosNoChangeDir     = 0x8

	sigdnFileSysPath = 0x80058000
	hrErrorCancelled = 0x800704C6 // HRESULT_FROM_WIN32(ERROR_CANCELLED)
	dialogClassName  = "#32770"
	swShow           = 5
	swpNomoveSize    = 0x3 // SWP_NOMOVE | SWP_NOSIZE
)

// IFileDialog vtable 索引（IUnknown 0-2 / IModalWindow.Show 3）
const (
	idxRelease    = 2
	idxShow       = 3
	idxSetOptions = 9
	idxSetFolder  = 12
	idxSetTitle   = 17
	idxGetResult  = 20
)

// IShellItem vtable 索引
const (
	idxGetDisplayName = 5
)

var (
	ole32                           = syscall.NewLazyDLL("ole32.dll")
	procCoInitializeEx              = ole32.NewProc("CoInitializeEx")
	procCoUninitialize              = ole32.NewProc("CoUninitialize")
	procCoCreateInstance            = ole32.NewProc("CoCreateInstance")
	procCoTaskMemFree               = ole32.NewProc("CoTaskMemFree")
	procSHCreateItemFromParsingName = syscall.NewLazyDLL("shell32.dll").NewProc("SHCreateItemFromParsingName")

	user32                       = syscall.NewLazyDLL("user32.dll")
	procEnumWindows              = user32.NewProc("EnumWindows")
	procGetClassNameW            = user32.NewProc("GetClassNameW")
	procIsWindowVisible          = user32.NewProc("IsWindowVisible")
	procGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
	procGetForegroundWindow      = user32.NewProc("GetForegroundWindow")
	procSetForegroundWindow      = user32.NewProc("SetForegroundWindow")
	procBringWindowToTop         = user32.NewProc("BringWindowToTop")
	procShowWindow               = user32.NewProc("ShowWindow")
	procSetWindowPos             = user32.NewProc("SetWindowPos")
	procAttachThreadInput        = user32.NewProc("AttachThreadInput")
	procGetCurrentThreadId       = syscall.NewLazyDLL("kernel32.dll").NewProc("GetCurrentThreadId")
)

var (
	clsidFileOpenDialog = syscall.GUID{
		Data1: 0xDC1C5A9C, Data2: 0xE88A, Data3: 0x4DDE,
		Data4: [8]byte{0xA5, 0xA1, 0x60, 0xF8, 0x2A, 0x20, 0xAE, 0xF7},
	}
	iidIFileDialog = syscall.GUID{
		Data1: 0xD57C7288, Data2: 0xD4AD, Data3: 0x4768,
		Data4: [8]byte{0xBE, 0x02, 0x9D, 0x96, 0x95, 0x32, 0xD9, 0x60},
	}
	iidIShellItem = syscall.GUID{
		Data1: 0x43826D1E, Data2: 0xE718, Data3: 0x42EE,
		Data4: [8]byte{0xBC, 0x55, 0xA1, 0xE2, 0x61, 0xC3, 0x7B, 0xFE},
	}
)

// comCall 按索引调用 COM 对象 vtable 方法。
func comCall(obj unsafe.Pointer, idx int, args ...uintptr) uintptr {
	vtbl := *(*unsafe.Pointer)(obj)
	fn := *(*uintptr)(unsafe.Add(vtbl, unsafe.Sizeof(uintptr(0))*uintptr(idx)))
	p := make([]uintptr, len(args)+1)
	p[0] = uintptr(obj)
	copy(p[1:], args)
	r1, _, _ := syscall.SyscallN(fn, p...)
	return r1
}

func utf16PtrToString(p *uint16) string {
	if p == nil {
		return ""
	}
	n := 0
	for ptr := unsafe.Pointer(p); *(*uint16)(ptr) != 0; n++ {
		ptr = unsafe.Add(ptr, 2)
	}
	return syscall.UTF16ToString(unsafe.Slice(p, n))
}

// pickFolder 弹出系统目录选择框，defaultDir 为弹框初始定位目录。
// 返回选中的目录；用户取消时返回空串和 nil error。
func pickFolder(defaultDir string) (string, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	hr, _, _ := procCoInitializeEx.Call(0, coinitApartmentThreaded)
	if hr != 0 && hr != 1 {
		return "", fmt.Errorf("CoInitializeEx 失败: 0x%08x", hr)
	}
	defer procCoUninitialize.Call()

	var dlg unsafe.Pointer
	hr, _, _ = procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidFileOpenDialog)),
		0, clsctxInprocServer,
		uintptr(unsafe.Pointer(&iidIFileDialog)),
		uintptr(unsafe.Pointer(&dlg)),
	)
	if hr != 0 || dlg == nil {
		return "", fmt.Errorf("创建文件对话框失败: 0x%08x", hr)
	}
	defer comCall(dlg, idxRelease)

	comCall(dlg, idxSetOptions, fosPickFolders|fosForceFilesystem|fosPathMustExist|fosNoChangeDir)

	if defaultDir != "" {
		w, err := syscall.UTF16PtrFromString(defaultDir)
		if err == nil {
			var psi unsafe.Pointer
			hr, _, _ = procSHCreateItemFromParsingName.Call(
				uintptr(unsafe.Pointer(w)),
				0,
				uintptr(unsafe.Pointer(&iidIShellItem)),
				uintptr(unsafe.Pointer(&psi)),
			)
			if hr == 0 && psi != nil {
				comCall(dlg, idxSetFolder, uintptr(psi))
				comCall(psi, idxRelease)
			}
		}
	}

	title, _ := syscall.UTF16PtrFromString("选择 zread 工作区")
	comCall(dlg, idxSetTitle, uintptr(unsafe.Pointer(title)))

	// 无属主窗口的对话框可能被压在其他窗口后面，辅助线程强制将其带到前台
	go bringDialogToFront(uint32(os.Getpid()))

	hr = comCall(dlg, idxShow, 0)
	if hr == hrErrorCancelled {
		log.Printf("目录选择框: 用户取消 (hr=0x%08x)", hr)
		return "", nil
	}
	if hr != 0 {
		return "", fmt.Errorf("目录选择框异常退出: 0x%08x", hr)
	}

	var item unsafe.Pointer
	hr = comCall(dlg, idxGetResult, uintptr(unsafe.Pointer(&item)))
	if hr != 0 || item == nil {
		return "", nil
	}
	defer comCall(item, idxRelease)

	var pathPtr *uint16
	hr = comCall(item, idxGetDisplayName, sigdnFileSysPath, uintptr(unsafe.Pointer(&pathPtr)))
	if hr != 0 || pathPtr == nil {
		return "", nil
	}
	defer procCoTaskMemFree.Call(uintptr(unsafe.Pointer(pathPtr)))

	return utf16PtrToString(pathPtr), nil
}

// findDialog 在本进程内查找对话框窗口（类名 #32770，可能尚未可见）。
func findDialog(pid uint32) uintptr {
	var found uintptr
	cb := syscall.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		var wpid uint32
		procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&wpid)))
		if wpid != pid {
			return 1
		}
		var buf [16]uint16
		n, _, _ := procGetClassNameW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), 16)
		if syscall.UTF16ToString(buf[:n]) != dialogClassName {
			return 1
		}
		found = hwnd
		return 0
	})
	procEnumWindows.Call(cb, 0)
	return found
}

// forceForeground 通过 AttachThreadInput 获取前台权限，把窗口拉到最前。
func forceForeground(hwnd uintptr) {
	cur, _, _ := procGetCurrentThreadId.Call()
	if fg, _, _ := procGetForegroundWindow.Call(); fg != 0 {
		if t, _, _ := procGetWindowThreadProcessId.Call(fg, 0); t != 0 {
			procAttachThreadInput.Call(cur, t, 1)
			defer procAttachThreadInput.Call(cur, t, 0)
		}
	}
	procShowWindow.Call(hwnd, swShow)
	procSetForegroundWindow.Call(hwnd)
	procBringWindowToTop.Call(hwnd)
	// TOPMOST→NOTOPMOST 闪烁，即使前台权限被拒也保证窗口可见置顶
	procSetWindowPos.Call(hwnd, ^uintptr(0), 0, 0, 0, 0, swpNomoveSize)
	procSetWindowPos.Call(hwnd, ^uintptr(1), 0, 0, 0, 0, swpNomoveSize)
}

// bringDialogToFront 等待对话框出现并强制可见/前台，避免无前台权限时对话框保持隐藏。
func bringDialogToFront(pid uint32) {
	runtime.LockOSThread()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(120 * time.Millisecond)
		hwnd := findDialog(pid)
		if hwnd == 0 {
			continue
		}
		forceForeground(hwnd)
		if fg, _, _ := procGetForegroundWindow.Call(); fg == hwnd {
			log.Printf("目录选择框已置前台: hwnd=0x%x", hwnd)
			return
		}
	}
	log.Printf("目录选择框未能置前台（超时）")
}
