package win32

import (
	"syscall"
	"unsafe"
)

// 常量、结构体、DLL/proc 表与通用小工具。
// x/sys/windows 未绑定的部分（ListView/InitCommonControlsEx 等）在此手写；
// 结构体布局在 init() 里做自检，偏移写错不会编译报错、只会静默内存错乱。

// ---------- DLL / proc ----------

var (
	User32   = syscall.NewLazyDLL("user32.dll")
	Gdi32    = syscall.NewLazyDLL("gdi32.dll")
	Comctl32 = syscall.NewLazyDLL("comctl32.dll")
	Kernel32 = syscall.NewLazyDLL("kernel32.dll")
	Shell32  = syscall.NewLazyDLL("shell32.dll")

	GetModuleHandleW   = Kernel32.NewProc("GetModuleHandleW")
	GetCurrentThreadId = Kernel32.NewProc("GetCurrentThreadId")
	CreateMutexW       = Kernel32.NewProc("CreateMutexW")
	LoadLibraryW       = Kernel32.NewProc("LoadLibraryW")

	RegisterClassExW              = User32.NewProc("RegisterClassExW")
	CreateWindowExW               = User32.NewProc("CreateWindowExW")
	DefWindowProcW                = User32.NewProc("DefWindowProcW")
	DestroyWindow                 = User32.NewProc("DestroyWindow")
	ShowWindow                    = User32.NewProc("ShowWindow")
	UpdateWindow                  = User32.NewProc("UpdateWindow")
	GetMessageW                   = User32.NewProc("GetMessageW")
	TranslateMessage              = User32.NewProc("TranslateMessage")
	DispatchMessageW              = User32.NewProc("DispatchMessageW")
	PostQuitMessage               = User32.NewProc("PostQuitMessage")
	PostMessageW                  = User32.NewProc("PostMessageW")
	SendMessageW                  = User32.NewProc("SendMessageW")
	MoveWindow                    = User32.NewProc("MoveWindow")
	SetWindowPos                  = User32.NewProc("SetWindowPos")
	GetClientRect                 = User32.NewProc("GetClientRect")
	GetWindowRect                 = User32.NewProc("GetWindowRect")
	SetWindowTextW                = User32.NewProc("SetWindowTextW")
	LoadCursorW                   = User32.NewProc("LoadCursorW")
	LoadIconW                     = User32.NewProc("LoadIconW")
	SetForegroundWindow           = User32.NewProc("SetForegroundWindow")
	FindWindowW                   = User32.NewProc("FindWindowW")
	IsIconic                      = User32.NewProc("IsIconic")
	SetFocus                      = User32.NewProc("SetFocus")
	EnableWindow                  = User32.NewProc("EnableWindow")
	IsWindow                      = User32.NewProc("IsWindow")
	GetCursorPos                  = User32.NewProc("GetCursorPos")
	CreatePopupMenu               = User32.NewProc("CreatePopupMenu")
	AppendMenuW                   = User32.NewProc("AppendMenuW")
	DestroyMenu                   = User32.NewProc("DestroyMenu")
	TrackPopupMenu                = User32.NewProc("TrackPopupMenu")
	MessageBoxW                   = User32.NewProc("MessageBoxW")
	GetDpiForWindow               = User32.NewProc("GetDpiForWindow")
	SetProcessDpiAwarenessContext = User32.NewProc("SetProcessDpiAwarenessContext")
	SetProcessDPIAware            = User32.NewProc("SetProcessDPIAware")
	GetDC                         = User32.NewProc("GetDC")
	ReleaseDC                     = User32.NewProc("ReleaseDC")
	GetSystemMetrics              = User32.NewProc("GetSystemMetrics")

	GetDeviceCaps = Gdi32.NewProc("GetDeviceCaps")
	CreateFontW   = Gdi32.NewProc("CreateFontW")
	DeleteObject  = Gdi32.NewProc("DeleteObject")

	InitCommonControlsEx = Comctl32.NewProc("InitCommonControlsEx")

	ShellNotifyIconW = Shell32.NewProc("Shell_NotifyIconW")

	LoadImageW             = User32.NewProc("LoadImageW")
	RegisterWindowMessageW = User32.NewProc("RegisterWindowMessageW")
	IsWindowVisible        = User32.NewProc("IsWindowVisible")
	IsDialogMessageW       = User32.NewProc("IsDialogMessageW")
)

// ---------- 窗口/消息 ----------

const (
	WS_OVERLAPPEDWINDOW = 0x00CF0000
	WS_CHILD            = 0x40000000
	WS_VISIBLE          = 0x10000000
	WS_CLIPSIBLINGS     = 0x04000000
	WS_CAPTION          = 0x00C00000
	WS_SYSMENU          = 0x00080000
	WS_TABSTOP          = 0x00010000
	WS_EX_CLIENTEDGE    = 0x00000200
	CW_USEDEFAULT       = uint32(0x80000000)

	SW_SHOW      = 5
	SW_RESTORE   = 9
	SW_HIDE      = 0
	SWP_NOZORDER = 0x0004
	IDC_ARROW    = 32512

	WS_VSCROLL   = 0x00200000
	COLOR_WINDOW = 5 // hbrBackground 传 COLOR_WINDOW+1
	LOGPIXELSX   = 88
	SM_CXVSCROLL = 2

	WM_NULL          = 0x0000
	WM_CREATE        = 0x0001
	WM_DESTROY       = 0x0002
	WM_SIZE          = 0x0005
	WM_SETREDRAW     = 0x000B
	WM_SETTEXT       = 0x000C
	WM_GETTEXT       = 0x000D
	WM_GETTEXTLENGTH = 0x000E
	WM_CLOSE         = 0x0010
	WM_SETFONT       = 0x0030
	WM_SETICON       = 0x0080
	WM_COMMAND       = 0x0111
	WM_NOTIFY        = 0x004E
	WM_DPICHANGED    = 0x02E0

	WM_APP = 0x8000

	MB_ICONERROR = 0x00000010
	MB_ICONWARN  = 0x00000030
	MB_ICONINFO  = 0x00000040

	ERROR_ALREADY_EXISTS = 183 // ERROR_ALREADY_EXISTS
)

// ---------- RichEdit（msftedit.dll）----------

const (
	ES_MULTILINE   = 0x0004
	ES_AUTOVSCROLL = 0x0040
	ES_READONLY    = 0x0800

	EM_SETSEL    = 0x0401 // WM_USER+1
	EM_SETTEXTEX = 0x0461 // WM_USER+97, RichEdit 3.0+

	ST_RTF = 4
)

// SetTextEx 是 EM_SETTEXTEX 的参数结构。
// 注意：富文本灌入必须用 EM_SETTEXTEX(ST_RTF) 而不是 EM_STREAMIN——
// RichEdit 内部调用流回调会触发 fail-fast(0xC0000409) 硬崩（RichEdit→Go
// thunk 路径，user32 直调回调不受影响），EM_SETTEXTEX 直接传字符串无回调。
type SetTextEx struct {
	Flags    uint32
	CodePage uint32
}

// ---------- 托盘（Shell_NotifyIconW）----------

const (
	NIM_ADD    = 0
	NIM_MODIFY = 1
	NIM_DELETE = 2

	NIF_MESSAGE = 0x01
	NIF_ICON    = 0x02
	NIF_TIP     = 0x04

	WM_LBUTTONUP     = 0x0202 // 托盘回调用 legacy 版本语义：lParam 即鼠标消息
	WM_RBUTTONUP     = 0x0205
	WM_LBUTTONDBLCLK = 0x0203

	SM_CXSMICON = 49
	SM_CYSMICON = 50

	IMAGE_ICON      = 1
	LR_DEFAULTCOLOR = 0
)

// NotifyIconDataW 布局按 x64 手排（cbSize=976），init() 自检兜底。
type NotifyIconDataW struct {
	CbSize           uint32
	_                uint32 // 对齐
	Hwnd             uintptr
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	_                uint32 // 对齐
	HIcon            uintptr
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UVersion         uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	Guid             [16]byte
	HBalloonIcon     uintptr
}

// ---------- 工具栏 ----------

const (
	TB_ENABLEBUTTON     = 0x0401 // WM_USER+1
	TB_BUTTONSTRUCTSIZE = 0x041E // WM_USER+30
	TB_AUTOSIZE         = 0x0421 // WM_USER+33
	TB_ADDBUTTONS       = 0x0444 // WM_USER+68
	TB_ADDSTRING        = 0x044D // WM_USER+77

	TBSTATE_ENABLED   = 0x04
	TBSTYLE_BUTTON    = 0x00
	TBSTYLE_SEP       = 0x01
	TBSTYLE_AUTOSIZE  = 0x10
	TBSTYLE_FLAT      = 0x0800
	CCS_NODIVIDER     = 0x40
	CCS_NORESIZE      = 0x4
	CCS_NOPARENTALIGN = 0x8
	I_IMAGENAME_NONE  = int32(-2)
)

// ---------- ListView ----------

const (
	LVS_REPORT        = 0x0001
	LVS_SINGLESEL     = 0x0004
	LVS_SHOWSELALWAYS = 0x0008

	LVS_EX_CHECKBOXES    = 0x00000004
	LVS_EX_FULLROWSELECT = 0x00000020
	LVS_EX_LABELTIP      = 0x00000400
	LVS_EX_DOUBLEBUFFER  = 0x00010000

	LVM_FIRST                    = 0x1000
	LVM_GETNEXTITEM              = LVM_FIRST + 12
	LVM_DELETEALLITEMS           = LVM_FIRST + 9
	LVM_SETEXTENDEDLISTVIEWSTYLE = LVM_FIRST + 54
	LVM_SETITEMSTATE             = LVM_FIRST + 43
	LVM_GETITEMSTATE             = LVM_FIRST + 44
	LVM_SETCOLUMNWIDTH           = LVM_FIRST + 30
	LVM_SETITEMW                 = LVM_FIRST + 76
	LVM_INSERTITEMW              = LVM_FIRST + 77
	LVM_INSERTCOLUMNW            = LVM_FIRST + 97

	LVIF_TEXT           = 0x0001
	LVIF_STATE          = 0x0008
	LVIS_SELECTED       = 0x0002
	LVIS_STATEIMAGEMASK = 0xF000
	LVNI_SELECTED       = 0x0002
	LVCF_FMT            = 0x0001
	LVCF_WIDTH          = 0x0002
	LVCF_TEXT           = 0x0004
	LVCFMT_LEFT         = 0x0000

	NM_DBLCLK       = -3
	NM_RCLICK       = -5
	NM_CUSTOMDRAW   = -12
	LVN_ITEMCHANGED = -101

	CDRF_DODEFAULT      = 0x00000000
	CDRF_NOTIFYITEMDRAW = 0x00000020
	CDDS_PREPAINT       = 0x00000001
	CDDS_ITEM           = 0x00010000
	CDDS_ITEMPREPAINT   = CDDS_ITEM | CDDS_PREPAINT
	CDDS_SUBITEM        = 0x00020000

	SB_SETTEXT = 0x040B // WM_USER+11

	MF_STRING       = 0x0000
	MF_GRAYED       = 0x0001
	MF_SEPARATOR    = 0x0800
	TPM_RIGHTBUTTON = 0x0002

	IDCANCEL = 2 // IsDialogMessage 按 Esc 产生的 IDCANCEL 命令
)

// ---------- ComboBox / Edit / Button ----------

const (
	CBS_DROPDOWNLIST = 0x0003

	CB_ADDSTRING = 0x0143
	CB_GETCURSEL = 0x0147
	CB_SETCURSEL = 0x014E

	CBN_SELCHANGE = 1 // WM_COMMAND HIWORD：下拉选中项变化

	ES_AUTOHSCROLL   = 0x0080
	BS_DEFPUSHBUTTON = 0x0001
)

// ---------- 字体 ----------

const (
	FW_NORMAL           = 400
	DEFAULT_CHARSET     = 1
	OUT_DEFAULT_PRECIS  = 0
	CLIP_DEFAULT_PRECIS = 0
	CLEARTYPE_QUALITY   = 5
	DEFAULT_PITCH       = 0
)

// ---------- 结构体 ----------

type Rect struct {
	Left, Top, Right, Bottom int32
}

type Point struct {
	X, Y int32
}

type Msg struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      Point
}

type WndClassExW struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       uintptr
}

// LVItemW / LVColumnW 布局已实测核对（PszText=24, LParam=40, PUColumns=64；PszText=16, ISubItem=28）。
type LVItemW struct {
	Mask       uint32
	IItem      int32
	ISubItem   int32
	State      uint32
	StateMask  uint32
	PszText    *uint16
	CchTextMax int32
	IImage     int32
	LParam     uintptr
	IIndent    int32
	IGroupID   int32
	CColumns   uint32
	PUColumns  *uint32
	PiColFmt   int32
	IGroup     int32
}

type LVColumnW struct {
	Mask       uint32
	Fmt        int32
	Cx         int32
	PszText    *uint16
	CchTextMax int32
	ISubItem   int32
	IImage     int32
	IOrder     int32
	CxMin      int32
	CxDefault  int32
	CxIdeal    int32
}

type NMHdr struct {
	HwndFrom uintptr
	IdFrom   uintptr
	Code     int32
}

type NMListView struct {
	Hdr       NMHdr
	IItem     int32
	ISubItem  int32
	UNewState uint32
	UOldState uint32
	UChanged  uint32
	PtAction  Point
	LParam    uintptr
}

type NMCustomDrawInfo struct {
	Hdr         NMHdr
	DwDrawStage uint32
	Hdc         uintptr
	Rc          Rect
	DwItemSpec  uintptr
	UItemState  uint32
	LItemlParam uintptr
}

type NMLVCustomDraw struct {
	Nmcd       NMCustomDrawInfo
	ClrText    uint32
	ClrTextBk  uint32
	ISubItem   int32
	UItemState uint32
	LParam     uintptr
}

type TBButton struct {
	IBitmap   int32
	IdCommand int32
	FsState   uint8
	FsStyle   uint8
	BReserved [6]uint8
	DwData    uintptr
	IString   uintptr
}

type InitCommonControlsExStr struct {
	DwSize uint32
	DwICC  uint32
}

const (
	ICC_LISTVIEW_CLASSES = 0x1
	ICC_BAR_CLASSES      = 0x4
)

func init() {
	if unsafe.Sizeof(LVItemW{}) != 80 || unsafe.Offsetof(LVItemW{}.PszText) != 24 ||
		unsafe.Offsetof(LVItemW{}.LParam) != 40 || unsafe.Offsetof(LVItemW{}.PUColumns) != 64 {
		panic("LVITEMW 布局与 Windows SDK 不一致")
	}
	if unsafe.Sizeof(LVColumnW{}) != 56 || unsafe.Offsetof(LVColumnW{}.PszText) != 16 ||
		unsafe.Offsetof(LVColumnW{}.ISubItem) != 28 {
		panic("LVCOLUMNW 布局与 Windows SDK 不一致")
	}
	if unsafe.Sizeof(TBButton{}) != 32 || unsafe.Sizeof(NMHdr{}) != 24 ||
		unsafe.Sizeof(NMCustomDrawInfo{}) != 80 || unsafe.Sizeof(WndClassExW{}) != 80 ||
		unsafe.Sizeof(Msg{}) != 48 || unsafe.Sizeof(NMListView{}) != 64 {
		panic("Win32 结构体布局与预期不一致")
	}
	if unsafe.Sizeof(SetTextEx{}) != 8 {
		panic("SETTEXTEX 布局与 Windows SDK 不一致")
	}
	if unsafe.Sizeof(NotifyIconDataW{}) != 976 ||
		unsafe.Offsetof(NotifyIconDataW{}.Hwnd) != 8 ||
		unsafe.Offsetof(NotifyIconDataW{}.HIcon) != 32 ||
		unsafe.Offsetof(NotifyIconDataW{}.SzTip) != 40 ||
		unsafe.Offsetof(NotifyIconDataW{}.SzInfo) != 304 ||
		unsafe.Offsetof(NotifyIconDataW{}.SzInfoTitle) != 820 ||
		unsafe.Offsetof(NotifyIconDataW{}.HBalloonIcon) != 968 {
		panic("NOTIFYICONDATAW 布局与 Windows SDK 不一致")
	}
}

// ---------- 小工具 ----------

func MustUTF16(s string) *uint16 {
	p, err := syscall.UTF16PtrFromString(s)
	if err != nil {
		panic("字符串包含非法字符 NUL: " + s)
	}
	return p
}

// UTF16DoubleNull 生成多个 \0 分隔、整体再以 \0 结尾的字符串缓冲（TB_ADDSTRING 用）。
func UTF16DoubleNull(parts ...string) []uint16 {
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

func UTF16PtrToString(p *uint16) string {
	if p == nil {
		return ""
	}
	n := 0
	for ptr := unsafe.Pointer(p); *(*uint16)(ptr) != 0; n++ {
		ptr = unsafe.Add(ptr, 2)
	}
	return syscall.UTF16ToString(unsafe.Slice(p, n))
}

func SendMsg(hwnd uintptr, msg uint32, wp, lp uintptr) uintptr {
	r, _, _ := SendMessageW.Call(hwnd, uintptr(msg), wp, lp)
	return r
}

func Loword(v uintptr) int { return int(int32(v) & 0xFFFF) }
func Hiword(v uintptr) int { return int(int32(v) >> 16) }

func Makelparam(lo, hi int) uintptr {
	return uintptr(uint32(uint16(lo)) | uint32(uint16(hi))<<16)
}

func RGB(r, g, b uint32) uint32 { return r | g<<8 | b<<16 }

// PtrFromLparam 把 wndproc 的 lParam 还原成指针。vet 不接受直接的 unsafe.Pointer(uintptr)
// 转换，经由 *unsafe.Pointer 间接完成。
// 注意：仅用于需要写回的场景（NM_CUSTOMDRAW 要改 ClrText），只读场景用 LparamCopy。
func PtrFromLparam(v uintptr) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&v))
}

// LparamCopy 把 lParam 指向的结构体拷贝成值返回，避免外部指针成为 GC 可追踪引用。
func LparamCopy[T any](lp uintptr) T {
	var v T
	src := *(*unsafe.Pointer)(unsafe.Pointer(&lp))
	dst := unsafe.Pointer(&v)
	copy(unsafe.Slice((*byte)(dst), unsafe.Sizeof(v)), unsafe.Slice((*byte)(src), unsafe.Sizeof(v)))
	return v
}

func MsgBox(hwnd uintptr, title, text string, flags uint32) {
	MessageBoxW.Call(hwnd,
		uintptr(unsafe.Pointer(MustUTF16(text))),
		uintptr(unsafe.Pointer(MustUTF16(title))),
		uintptr(flags))
}

// LvSetItemText 写单元格；sub==0 时插入新行。
func LvSetItemText(hwnd uintptr, idx, sub int, text string) {
	buf := MustUTF16(text)
	it := LVItemW{Mask: LVIF_TEXT, IItem: int32(idx), ISubItem: int32(sub), PszText: buf}
	if sub == 0 {
		SendMsg(hwnd, LVM_INSERTITEMW, 0, uintptr(unsafe.Pointer(&it)))
	} else {
		SendMsg(hwnd, LVM_SETITEMW, 0, uintptr(unsafe.Pointer(&it)))
	}
}

func LvSelected(hwnd uintptr) int {
	r := SendMsg(hwnd, LVM_GETNEXTITEM, ^uintptr(0), LVNI_SELECTED)
	return int(int32(r))
}

func ClientHeight(hwnd uintptr) int {
	var rc Rect
	GetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&rc)))
	return int(rc.Bottom - rc.Top)
}
