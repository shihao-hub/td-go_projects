//go:build windows

package main

// COM 文件/目录选择框（IFileDialog），自 zread-tray/pickfolder_windows.go 改造：
// 选文件 + 过滤器；对话框有属主窗口，不再需要前台抢焦点辅助线程。
// IFileDialog vtable 索引（IUnknown 0-2 / IModalWindow.Show 3）已对照 SDK 头文件核对。

import (
	"fmt"
	"syscall"
	"unsafe"
)

const (
	coinitApartmentThreaded = 0x2
	clsctxInprocServer      = 0x1

	fosPickFolders     = 0x20
	fosForceFilesystem = 0x40
	fosPathMustExist   = 0x800
	fosNoChangeDir     = 0x8
	fosFileMustExist   = 0x1000

	sigdnFileSysPath = 0x80058000
	hrErrorCancelled = 0x800704C6 // HRESULT_FROM_WIN32(ERROR_CANCELLED)
)

// IFileDialog vtable 索引
const (
	dlgRelease         = 2
	dlgShow            = 3
	dlgSetFileTypes    = 4
	dlgSetOptions      = 9
	dlgSetFolder       = 12
	dlgSetTitle        = 17
	dlgGetResult       = 20
	itemGetDisplayName = 5 // IShellItem
)

var (
	ole32                           = syscall.NewLazyDLL("ole32.dll")
	procCoInitializeEx              = ole32.NewProc("CoInitializeEx")
	procCoUninitialize              = ole32.NewProc("CoUninitialize")
	procCoCreateInstance            = ole32.NewProc("CoCreateInstance")
	procCoTaskMemFree               = ole32.NewProc("CoTaskMemFree")
	procSHCreateItemFromParsingName = syscall.NewLazyDLL("shell32.dll").NewProc("SHCreateItemFromParsingName")
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

// comDlgFilterSpec 对应 COMDLG_FILTERSPEC。
type comDlgFilterSpec struct {
	name *uint16
	spec *uint16
}

type fileDialogOptions struct {
	owner      uintptr
	title      string
	pickDir    bool
	fileTypes  []comDlgFilterSpec
	defaultDir string
}

// runFileDialog 弹出系统文件/目录选择框；用户取消返回 ("", nil)。
func runFileDialog(o fileDialogOptions) (string, error) {
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
	defer comCall(dlg, dlgRelease)

	fos := uint32(fosForceFilesystem | fosPathMustExist | fosNoChangeDir)
	if o.pickDir {
		fos |= fosPickFolders
	} else {
		fos |= fosFileMustExist
	}
	comCall(dlg, dlgSetOptions, uintptr(fos))

	if !o.pickDir && len(o.fileTypes) > 0 {
		comCall(dlg, dlgSetFileTypes, uintptr(len(o.fileTypes)), uintptr(unsafe.Pointer(&o.fileTypes[0])))
	}

	if o.defaultDir != "" {
		w, err := syscall.UTF16PtrFromString(o.defaultDir)
		if err == nil {
			var psi unsafe.Pointer
			hr, _, _ = procSHCreateItemFromParsingName.Call(
				uintptr(unsafe.Pointer(w)),
				0,
				uintptr(unsafe.Pointer(&iidIShellItem)),
				uintptr(unsafe.Pointer(&psi)),
			)
			if hr == 0 && psi != nil {
				comCall(dlg, dlgSetFolder, uintptr(psi))
				comCall(psi, dlgRelease)
			}
		}
	}

	if o.title != "" {
		comCall(dlg, dlgSetTitle, uintptr(unsafe.Pointer(mustUTF16(o.title))))
	}

	hr = comCall(dlg, dlgShow, o.owner)
	if hr == hrErrorCancelled {
		return "", nil
	}
	if hr != 0 {
		return "", fmt.Errorf("对话框异常退出: 0x%08x", hr)
	}

	var item unsafe.Pointer
	hr = comCall(dlg, dlgGetResult, uintptr(unsafe.Pointer(&item)))
	if hr != 0 || item == nil {
		return "", nil
	}
	defer comCall(item, dlgRelease)

	var pathPtr *uint16
	hr = comCall(item, itemGetDisplayName, sigdnFileSysPath, uintptr(unsafe.Pointer(&pathPtr)))
	if hr != 0 || pathPtr == nil {
		return "", nil
	}
	defer procCoTaskMemFree.Call(uintptr(unsafe.Pointer(pathPtr)))

	return utf16PtrToString(pathPtr), nil
}

// pickExeFile 选择单个 exe 文件。
func pickExeFile(owner uintptr, defaultDir string) (string, error) {
	specs := []comDlgFilterSpec{
		{mustUTF16("程序 (*.exe)"), mustUTF16("*.exe")},
		{mustUTF16("所有文件 (*.*)"), mustUTF16("*.*")},
	}
	return runFileDialog(fileDialogOptions{
		owner:      owner,
		title:      "选择 EXE",
		fileTypes:  specs,
		defaultDir: defaultDir,
	})
}

// pickScanRoot 选择扫描根目录。
func pickScanRoot(owner uintptr, defaultDir string) (string, error) {
	return runFileDialog(fileDialogOptions{
		owner:      owner,
		title:      "选择扫描根目录",
		pickDir:    true,
		defaultDir: defaultDir,
	})
}
