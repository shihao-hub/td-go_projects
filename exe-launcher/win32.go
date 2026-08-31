package main

import (
	"syscall"
	"unsafe"
)

// 常量、结构体、DLL/proc 表与通用小工具。
// x/sys/windows 未绑定的部分（ListView/InitCommonControlsEx 等）在此手写；
// 结构体布局在 init() 里做自检，偏移写错不会编译报错、只会静默内存错乱。

// ---------- DLL / proc ----------

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	comctl32 = syscall.NewLazyDLL("comctl32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")

	pGetModuleHandleW   = kernel32.NewProc("GetModuleHandleW")
	pGetCurrentThreadId = kernel32.NewProc("GetCurrentThreadId")
	pCreateMutexW       = kernel32.NewProc("CreateMutexW")
	pLoadLibraryW       = kernel32.NewProc("LoadLibraryW")

	pRegisterClassExW              = user32.NewProc("RegisterClassExW")
	pCreateWindowExW               = user32.NewProc("CreateWindowExW")
	pDefWindowProcW                = user32.NewProc("DefWindowProcW")
	pDestroyWindow                 = user32.NewProc("DestroyWindow")
	pShowWindow                    = user32.NewProc("ShowWindow")
	pUpdateWindow                  = user32.NewProc("UpdateWindow")
	pGetMessageW                   = user32.NewProc("GetMessageW")
	pTranslateMessage              = user32.NewProc("TranslateMessage")
	pDispatchMessageW              = user32.NewProc("DispatchMessageW")
	pPostQuitMessage               = user32.NewProc("PostQuitMessage")
	pPostMessageW                  = user32.NewProc("PostMessageW")
	pSendMessageW                  = user32.NewProc("SendMessageW")
	pMoveWindow                    = user32.NewProc("MoveWindow")
	pSetWindowPos                  = user32.NewProc("SetWindowPos")
	pGetClientRect                 = user32.NewProc("GetClientRect")
	pGetWindowRect                 = user32.NewProc("GetWindowRect")
	pSetWindowTextW                = user32.NewProc("SetWindowTextW")
	pLoadCursorW                   = user32.NewProc("LoadCursorW")
	pLoadIconW                     = user32.NewProc("LoadIconW")
	pSetForegroundWindow           = user32.NewProc("SetForegroundWindow")
	pFindWindowW                   = user32.NewProc("FindWindowW")
	pIsIconic                      = user32.NewProc("IsIconic")
	pSetFocus                      = user32.NewProc("SetFocus")
	pEnableWindow                  = user32.NewProc("EnableWindow")
	pIsWindow                      = user32.NewProc("IsWindow")
	pGetCursorPos                  = user32.NewProc("GetCursorPos")
	pCreatePopupMenu               = user32.NewProc("CreatePopupMenu")
	pAppendMenuW                   = user32.NewProc("AppendMenuW")
	pDestroyMenu                   = user32.NewProc("DestroyMenu")
	pTrackPopupMenu                = user32.NewProc("TrackPopupMenu")
	pMessageBoxW                   = user32.NewProc("MessageBoxW")
	pGetDpiForWindow               = user32.NewProc("GetDpiForWindow")
	pSetProcessDpiAwarenessContext = user32.NewProc("SetProcessDpiAwarenessContext")
	pSetProcessDPIAware            = user32.NewProc("SetProcessDPIAware")
	pGetDC                         = user32.NewProc("GetDC")
	pReleaseDC                     = user32.NewProc("ReleaseDC")
	pGetSystemMetrics              = user32.NewProc("GetSystemMetrics")

	pGetDeviceCaps = gdi32.NewProc("GetDeviceCaps")
	pCreateFontW   = gdi32.NewProc("CreateFontW")
	pDeleteObject  = gdi32.NewProc("DeleteObject")

	pInitCommonControlsEx = comctl32.NewProc("InitCommonControlsEx")

	pShellNotifyIconW = shell32.NewProc("Shell_NotifyIconW")

	pLoadImageW             = user32.NewProc("LoadImageW")
	pRegisterWindowMessageW = user32.NewProc("RegisterWindowMessageW")
	pIsWindowVisible        = user32.NewProc("IsWindowVisible")
)

// ---------- 窗口/消息 ----------

const (
	wsOverlappedWindow = 0x00CF0000
	wsChild            = 0x40000000
	wsVisible          = 0x10000000
	wsClipSiblings     = 0x04000000
	wsCaption          = 0x00C00000
	wsSysMenu          = 0x00080000
	wsTabStop          = 0x00010000
	wsExClientEdge     = 0x00000200
	cwUseDefault       = uint32(0x80000000)

	swShow      = 5
	swRestore   = 9
	swHide      = 0
	swpNoZOrder = 0x0004
	idcArrow    = 32512

	wsVScroll   = 0x00200000
	colorWindow = 5 // hbrBackground 传 COLOR_WINDOW+1
	logPixelSX  = 88
	smCxVScroll = 2

	wmNull       = 0x0000
	wmCreate     = 0x0001
	wmDestroy    = 0x0002
	wmSize       = 0x0005
	wmSetRedraw  = 0x000B
	wmClose      = 0x0010
	wmSetFont    = 0x0030
	wmSetIcon    = 0x0080
	wmCommand    = 0x0111
	wmNotify     = 0x004E
	wmDpiChanged = 0x02E0

	wmApp = 0x8000

	mbIconError = 0x00000010
	mbIconWarn  = 0x00000030
	mbIconInfo  = 0x00000040

	errorAlreadyExists = 183 // ERROR_ALREADY_EXISTS
)

// ---------- RichEdit（msftedit.dll）----------

const (
	esMultiline   = 0x0004
	esAutoVScroll = 0x0040
	esReadOnly    = 0x0800

	emSetSel    = 0x0401 // WM_USER+1
	emSettextex = 0x0461 // WM_USER+97, RichEdit 3.0+

	stRtf = 4
)

// setTextEx 是 EM_SETTEXTEX 的参数结构。
// 注意：富文本灌入必须用 EM_SETTEXTEX(ST_RTF) 而不是 EM_STREAMIN——
// RichEdit 内部调用流回调会触发 fail-fast(0xC0000409) 硬崩（RichEdit→Go
// thunk 路径，user32 直调回调不受影响），EM_SETTEXTEX 直接传字符串无回调。
type setTextEx struct {
	flags    uint32
	codePage uint32
}

// ---------- 托盘（Shell_NotifyIconW）----------

const (
	nimAdd    = 0
	nimModify = 1
	nimDelete = 2

	nifMessage = 0x01
	nifIcon    = 0x02
	nifTip     = 0x04

	wmLbuttonUp  = 0x0202 // 托盘回调用 legacy 版本语义：lParam 即鼠标消息
	wmRbuttonUp  = 0x0205
	wmLbuttonDbl = 0x0203

	smCxsmIcon = 49
	smCysmIcon = 50

	imageIcon      = 1
	lrDefaultColor = 0
)

// notifyIconDataW 布局按 x64 手排（cbSize=976），init() 自检兜底。
type notifyIconDataW struct {
	cbSize           uint32
	_                uint32 // 对齐
	hWnd             uintptr
	uID              uint32
	uFlags           uint32
	uCallbackMessage uint32
	_                uint32 // 对齐
	hIcon            uintptr
	szTip            [128]uint16
	dwState          uint32
	dwStateMask      uint32
	szInfo           [256]uint16
	uVersion         uint32
	szInfoTitle      [64]uint16
	dwInfoFlags      uint32
	guid             [16]byte
	hBalloonIcon     uintptr
}

// ---------- 工具栏 ----------

const (
	tbEnableButton     = 0x0401 // WM_USER+1
	tbButtonStructSize = 0x041E // WM_USER+30
	tbAutosize         = 0x0421 // WM_USER+33
	tbAddButtons       = 0x0444 // WM_USER+68
	tbAddString        = 0x044D // WM_USER+77

	tbstateEnabled   = 0x04
	tbstyButton      = 0x00
	tbstySep         = 0x01
	tbstyAutosize    = 0x10
	tbstyFlat        = 0x0800
	ccsNoDivider     = 0x40
	ccsNoResize      = 0x4
	ccsNoParentAlign = 0x8
	iImagenameNone   = int32(-2)
)

// ---------- ListView ----------

const (
	lvsReport        = 0x0001
	lvsSingleSel     = 0x0004
	lvsShowSelAlways = 0x0008

	lvsExCheckBoxes    = 0x00000004
	lvsExFullRowSelect = 0x00000020
	lvsExLabelTip      = 0x00000400
	lvsExDoubleBuffer  = 0x00010000

	lvmFirst          = 0x1000
	lvmGetNextItem    = lvmFirst + 12
	lvmDeleteAllItems = lvmFirst + 9
	lvmSetExtStyle    = lvmFirst + 54
	lvmSetItemState   = lvmFirst + 43
	lvmGetItemState   = lvmFirst + 44
	lvmSetColumnWidth = lvmFirst + 30
	lvmSetItemW       = lvmFirst + 76
	lvmInsertItemW    = lvmFirst + 77
	lvmInsertColumnW  = lvmFirst + 97

	lvifText           = 0x0001
	lvifState          = 0x0008
	lvisSelected       = 0x0002
	lvisStateImageMask = 0xF000
	lvniSelected       = 0x0002
	lvcfFmt            = 0x0001
	lvcfWidth          = 0x0002
	lvcfText           = 0x0004
	lvcfmtLeft         = 0x0000

	nmDblclk       = -3
	nmRclick       = -5
	nmCustomDraw   = -12
	lvnItemChanged = -101

	cdrfDoDefault      = 0x00000000
	cdrfNotifyItemDraw = 0x00000020
	cddsPrepaint       = 0x00000001
	cddsItem           = 0x00010000
	cddsItemPrepaint   = cddsItem | cddsPrepaint

	sbSetText = 0x040B // WM_USER+11

	mfString       = 0x0000
	mfGrayed       = 0x0001
	mfSeparator    = 0x0800
	tpmRightButton = 0x0002
)

// ---------- 字体 ----------

const (
	fwNormal          = 400
	defaultCharset    = 1
	outDefaultPrecis  = 0
	clipDefaultPrecis = 0
	clearTypeQuality  = 5
	defaultPitch      = 0
)

// ---------- 结构体 ----------

type rect struct {
	left, top, right, bottom int32
}

type point struct {
	x, y int32
}

type msg struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      point
}

type wndClassExW struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     uintptr
	hIcon         uintptr
	hCursor       uintptr
	hbrBackground uintptr
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       uintptr
}

// lvItemW / lvColumnW 布局已实测核对（pszText=24, lParam=40, puColumns=64；pszText=16, iSubItem=28）。
type lvItemW struct {
	mask       uint32
	iItem      int32
	iSubItem   int32
	state      uint32
	stateMask  uint32
	pszText    *uint16
	cchTextMax int32
	iImage     int32
	lParam     uintptr
	iIndent    int32
	iGroupID   int32
	cColumns   uint32
	puColumns  *uint32
	piColFmt   int32
	iGroup     int32
}

type lvColumnW struct {
	mask       uint32
	fmt        int32
	cx         int32
	pszText    *uint16
	cchTextMax int32
	iSubItem   int32
	iImage     int32
	iOrder     int32
	cxMin      int32
	cxDefault  int32
	cxIdeal    int32
}

type nmHdr struct {
	hwndFrom uintptr
	idFrom   uintptr
	code     int32
}

type nmListView struct {
	hdr       nmHdr
	iItem     int32
	iSubItem  int32
	uNewState uint32
	uOldState uint32
	uChanged  uint32
	ptAction  point
	lParam    uintptr
}

type nmCustomDrawInfo struct {
	hdr         nmHdr
	dwDrawStage uint32
	hdc         uintptr
	rc          rect
	dwItemSpec  uintptr
	uItemState  uint32
	lItemlParam uintptr
}

type nmlvCustomDraw struct {
	nmcd       nmCustomDrawInfo
	clrText    uint32
	clrTextBk  uint32
	iSubItem   int32
	uItemState uint32
	lParam     uintptr
}

type tbButton struct {
	iBitmap   int32
	idCommand int32
	fsState   uint8
	fsStyle   uint8
	bReserved [6]uint8
	dwData    uintptr
	iString   uintptr
}

type initCommonControlsExStr struct {
	dwSize uint32
	dwICC  uint32
}

const (
	iccListviewClasses = 0x1
	iccBarClasses      = 0x4
)

func init() {
	if unsafe.Sizeof(lvItemW{}) != 80 || unsafe.Offsetof(lvItemW{}.pszText) != 24 ||
		unsafe.Offsetof(lvItemW{}.lParam) != 40 || unsafe.Offsetof(lvItemW{}.puColumns) != 64 {
		panic("LVITEMW 布局与 Windows SDK 不一致")
	}
	if unsafe.Sizeof(lvColumnW{}) != 56 || unsafe.Offsetof(lvColumnW{}.pszText) != 16 ||
		unsafe.Offsetof(lvColumnW{}.iSubItem) != 28 {
		panic("LVCOLUMNW 布局与 Windows SDK 不一致")
	}
	if unsafe.Sizeof(tbButton{}) != 32 || unsafe.Sizeof(nmHdr{}) != 24 ||
		unsafe.Sizeof(nmCustomDrawInfo{}) != 80 || unsafe.Sizeof(wndClassExW{}) != 80 ||
		unsafe.Sizeof(msg{}) != 48 || unsafe.Sizeof(nmListView{}) != 64 {
		panic("Win32 结构体布局与预期不一致")
	}
	if unsafe.Sizeof(setTextEx{}) != 8 {
		panic("SETTEXTEX 布局与 Windows SDK 不一致")
	}
	if unsafe.Sizeof(notifyIconDataW{}) != 976 ||
		unsafe.Offsetof(notifyIconDataW{}.hWnd) != 8 ||
		unsafe.Offsetof(notifyIconDataW{}.hIcon) != 32 ||
		unsafe.Offsetof(notifyIconDataW{}.szTip) != 40 ||
		unsafe.Offsetof(notifyIconDataW{}.szInfo) != 304 ||
		unsafe.Offsetof(notifyIconDataW{}.szInfoTitle) != 820 ||
		unsafe.Offsetof(notifyIconDataW{}.hBalloonIcon) != 968 {
		panic("NOTIFYICONDATAW 布局与 Windows SDK 不一致")
	}
}

// ---------- 小工具 ----------

func mustUTF16(s string) *uint16 {
	p, err := syscall.UTF16PtrFromString(s)
	if err != nil {
		panic("字符串包含非法字符 NUL: " + s)
	}
	return p
}

// utf16DoubleNull 生成多个 \0 分隔、整体再以 \0 结尾的字符串缓冲（TB_ADDSTRING 用）。
func utf16DoubleNull(parts ...string) []uint16 {
	var out []uint16
	for _, s := range parts {
		u, err := syscall.UTF16FromString(s)
		if err != nil {
			panic("字符串包含非法字符 NUL: " + s)
		}
		out = append(out, u...)
	}
	out = append(out, 0)
	return out
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

func sendMsg(hwnd uintptr, msg uint32, wp, lp uintptr) uintptr {
	r, _, _ := pSendMessageW.Call(hwnd, uintptr(msg), wp, lp)
	return r
}

func loword(v uintptr) int { return int(int32(v) & 0xFFFF) }
func hiword(v uintptr) int { return int(int32(v) >> 16) }

func makelparam(lo, hi int) uintptr {
	return uintptr(uint32(uint16(lo)) | uint32(uint16(hi))<<16)
}

func rgb(r, g, b uint32) uint32 { return r | g<<8 | b<<16 }

// ptrFromLparam 把 wndproc 的 lParam 还原成指针。vet 不接受直接的 unsafe.Pointer(uintptr)
// 转换，经由 *unsafe.Pointer 间接完成。
// 注意：仅用于需要写回的场景（NM_CUSTOMDRAW 要改 clrText），只读场景用 lparamCopy。
func ptrFromLparam(v uintptr) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&v))
}

// lparamCopy 把 lParam 指向的结构体拷贝成值返回，避免外部指针成为 GC 可追踪引用。
func lparamCopy[T any](lp uintptr) T {
	var v T
	src := *(*unsafe.Pointer)(unsafe.Pointer(&lp))
	dst := unsafe.Pointer(&v)
	copy(unsafe.Slice((*byte)(dst), unsafe.Sizeof(v)), unsafe.Slice((*byte)(src), unsafe.Sizeof(v)))
	return v
}

func msgBox(hwnd uintptr, title, text string, flags uint32) {
	pMessageBoxW.Call(hwnd,
		uintptr(unsafe.Pointer(mustUTF16(text))),
		uintptr(unsafe.Pointer(mustUTF16(title))),
		uintptr(flags))
}

// lvSetItemText 写单元格；sub==0 时插入新行。
func lvSetItemText(hwnd uintptr, idx, sub int, text string) {
	buf := mustUTF16(text)
	it := lvItemW{mask: lvifText, iItem: int32(idx), iSubItem: int32(sub), pszText: buf}
	if sub == 0 {
		sendMsg(hwnd, lvmInsertItemW, 0, uintptr(unsafe.Pointer(&it)))
	} else {
		sendMsg(hwnd, lvmSetItemW, 0, uintptr(unsafe.Pointer(&it)))
	}
}

func lvSelected(hwnd uintptr) int {
	r := sendMsg(hwnd, lvmGetNextItem, ^uintptr(0), lvniSelected)
	return int(int32(r))
}

func clientHeight(hwnd uintptr) int {
	var rc rect
	pGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&rc)))
	return int(rc.bottom - rc.top)
}
