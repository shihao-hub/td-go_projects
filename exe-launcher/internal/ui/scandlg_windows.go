package ui

import (
	"exe-launcher/internal/model"
	"exe-launcher/internal/win32"
	"syscall"
	"unsafe"
)

// 扫描结果导入对话框：带复选框的模态列表（LVS_EX_CHECKBOXES），
// 勾完点「导入」才入库 —— 不自动导入。

const (
	scnList = 201

	scnSelectAll  = 2001
	scnSelectNone = 2002
	scnImport     = 2003
	scnCancel     = 2004
)

const scanClassName = "ExeLauncherScanWnd"

type scanDialog struct {
	hwnd    uintptr
	hList   uintptr
	buttons [4]uintptr
	items   []string
	chosen  []string
	scale   float64
	hFont   uintptr
}

var (
	scanDlg       *scanDialog
	scanWndProcCb uintptr
)

// runScanDialog 弹出模态导入列表，返回勾选要导入的路径（取消返回 nil）。
func runScanDialog(owner *mainWindow, items []string) []string {
	hInst, _, _ := win32.GetModuleHandleW.Call(0)
	if scanWndProcCb == 0 {
		scanWndProcCb = syscall.NewCallback(scanWndProc)
		wc := win32.WndClassExW{
			CbSize:        uint32(unsafe.Sizeof(win32.WndClassExW{})),
			LpfnWndProc:   scanWndProcCb,
			HInstance:     hInst,
			HCursor:       loadArrowCursor(),
			HbrBackground: win32.COLOR_WINDOW + 1,
			LpszClassName: win32.MustUTF16(scanClassName),
		}
		if atom, _, _ := win32.RegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); atom == 0 {
			return nil
		}
	}

	d := &scanDialog{items: items, scale: owner.scale, hFont: owner.hFont}
	scanDlg = d

	width := int(700 * d.scale)
	height := int(480 * d.scale)
	var orc win32.Rect
	win32.GetWindowRect.Call(owner.hwnd, uintptr(unsafe.Pointer(&orc)))
	x := int(orc.Left) + (int(orc.Right-orc.Left)-width)/2
	y := int(orc.Top) + (int(orc.Bottom-orc.Top)-height)/2
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}

	hwnd, _, _ := win32.CreateWindowExW.Call(0,
		uintptr(unsafe.Pointer(win32.MustUTF16(scanClassName))),
		uintptr(unsafe.Pointer(win32.MustUTF16("导入扫描结果"))),
		win32.WS_CAPTION|win32.WS_SYSMENU,
		uintptr(uint32(x)), uintptr(uint32(y)),
		uintptr(uint32(width)), uintptr(uint32(height)),
		owner.hwnd, 0, hInst, 0)
	if hwnd == 0 {
		scanDlg = nil
		return nil
	}
	d.hwnd = hwnd

	d.hList, _, _ = win32.CreateWindowExW.Call(win32.WS_EX_CLIENTEDGE,
		uintptr(unsafe.Pointer(win32.MustUTF16("SysListView32"))),
		0, uintptr(win32.WS_CHILD|win32.WS_VISIBLE|win32.WS_TABSTOP|win32.LVS_REPORT|win32.LVS_SINGLESEL|win32.LVS_SHOWSELALWAYS),
		0, 0, 0, 0, hwnd, scnList, hInst, 0)
	win32.SendMsg(d.hList, win32.WM_SETFONT, d.hFont, 1)
	ext := uintptr(win32.LVS_EX_CHECKBOXES | win32.LVS_EX_FULLROWSELECT | win32.LVS_EX_DOUBLEBUFFER | win32.LVS_EX_LABELTIP)
	win32.SendMsg(d.hList, win32.LVM_SETEXTENDEDLISTVIEWSTYLE, ext, ext)
	for i, name := range []string{"名称", "路径"} {
		col := win32.LVColumnW{
			Mask:     win32.LVCF_FMT | win32.LVCF_WIDTH | win32.LVCF_TEXT,
			Fmt:      win32.LVCFMT_LEFT,
			Cx:       int32(180 * d.scale),
			PszText:  win32.MustUTF16(name),
			ISubItem: int32(i),
		}
		win32.SendMsg(d.hList, win32.LVM_INSERTCOLUMNW, uintptr(i), uintptr(unsafe.Pointer(&col)))
	}
	for i, p := range items {
		win32.LvSetItemText(d.hList, i, 0, model.ExeBaseName(p))
		win32.LvSetItemText(d.hList, i, 1, p)
	}
	d.setAllChecks(true)

	labels := []string{"全选", "全不选", "导入", "取消"}
	ids := []uintptr{scnSelectAll, scnSelectNone, scnImport, scnCancel}
	for i := range labels {
		d.buttons[i], _, _ = win32.CreateWindowExW.Call(0,
			uintptr(unsafe.Pointer(win32.MustUTF16("BUTTON"))),
			uintptr(unsafe.Pointer(win32.MustUTF16(labels[i]))),
			uintptr(win32.WS_CHILD|win32.WS_VISIBLE|win32.WS_TABSTOP),
			0, 0, 0, 0, hwnd, ids[i], hInst, 0)
		win32.SendMsg(d.buttons[i], win32.WM_SETFONT, d.hFont, 1)
	}

	d.layout()

	win32.EnableWindow.Call(owner.hwnd, 0)
	win32.ShowWindow.Call(hwnd, win32.SW_SHOW)
	win32.UpdateWindow.Call(hwnd)
	win32.SetFocus.Call(d.hList)

	// 模态消息循环：对话框销毁或收到 WM_QUIT（主窗口被关）时退出
	var m win32.Msg
	quit := false
	for {
		r, _, _ := win32.GetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) == 0 {
			quit = true
			break
		}
		if int32(r) == -1 {
			break
		}
		win32.TranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		win32.DispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
		if alive, _, _ := win32.IsWindow.Call(hwnd); alive == 0 {
			break
		}
	}

	win32.EnableWindow.Call(owner.hwnd, 1)
	win32.SetForegroundWindow.Call(owner.hwnd)
	scanDlg = nil
	if quit {
		win32.PostQuitMessage.Call(m.WParam)
		return nil
	}
	return d.chosen
}

func scanWndProc(hwnd uintptr, msg uint32, wp, lp uintptr) uintptr {
	defer recoverLog("扫描对话框 wndproc")
	d := scanDlg
	if d == nil || d.hwnd != hwnd {
		r, _, _ := win32.DefWindowProcW.Call(hwnd, uintptr(msg), wp, lp)
		return r
	}
	switch msg {
	case win32.WM_SIZE:
		d.layout()
		return 0
	case win32.WM_COMMAND:
		switch win32.Loword(wp) {
		case scnSelectAll:
			d.setAllChecks(true)
		case scnSelectNone:
			d.setAllChecks(false)
		case scnImport:
			d.chosen = d.checkedItems()
			win32.DestroyWindow.Call(hwnd)
		case scnCancel:
			win32.DestroyWindow.Call(hwnd)
		}
		return 0
	case win32.WM_CLOSE:
		win32.DestroyWindow.Call(hwnd)
		return 0
	}
	r, _, _ := win32.DefWindowProcW.Call(hwnd, uintptr(msg), wp, lp)
	return r
}

func (d *scanDialog) layout() {
	var rc win32.Rect
	win32.GetClientRect.Call(d.hwnd, uintptr(unsafe.Pointer(&rc)))
	cw := int(rc.Right - rc.Left)
	ch := int(rc.Bottom - rc.Top)
	if cw <= 0 || ch <= 0 {
		return
	}
	bh := int(30 * d.scale)
	bw := int(92 * d.scale)
	pad := int(10 * d.scale)
	margin := int(12 * d.scale)

	listH := ch - bh - 2*pad - margin
	if listH < 60 {
		listH = 60
	}
	win32.MoveWindow.Call(d.hList, uintptr(margin), uintptr(margin), uintptr(cw-2*margin), uintptr(listH), 1)

	y := ch - bh - pad
	x := cw - margin - bw
	for _, i := range []int{3, 2, 1, 0} { // 取消/导入/全不选/全选 从右往左
		win32.MoveWindow.Call(d.buttons[i], uintptr(x), uintptr(y), uintptr(bw), uintptr(bh), 1)
		x -= bw + pad
	}
}

// setAllChecks iItem=-1 应用到全部；2<<12=选中，1<<12=未选中。
func (d *scanDialog) setAllChecks(check bool) {
	state := uint32(1 << 12)
	if check {
		state = 2 << 12
	}
	it := win32.LVItemW{State: state, StateMask: win32.LVIS_STATEIMAGEMASK, IItem: -1}
	win32.SendMsg(d.hList, win32.LVM_SETITEMSTATE, ^uintptr(0), uintptr(unsafe.Pointer(&it)))
}

func (d *scanDialog) checkedItems() []string {
	var out []string
	for i := range d.items {
		st := win32.SendMsg(d.hList, win32.LVM_GETITEMSTATE, uintptr(i), win32.LVIS_STATEIMAGEMASK)
		if st&0xF000 == 2<<12 {
			out = append(out, d.items[i])
		}
	}
	return out
}
