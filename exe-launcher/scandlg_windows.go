package main

import (
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
	hInst, _, _ := pGetModuleHandleW.Call(0)
	if scanWndProcCb == 0 {
		scanWndProcCb = syscall.NewCallback(scanWndProc)
		wc := wndClassExW{
			cbSize:        uint32(unsafe.Sizeof(wndClassExW{})),
			lpfnWndProc:   scanWndProcCb,
			hInstance:     hInst,
			hCursor:       loadArrowCursor(),
			hbrBackground: colorWindow + 1,
			lpszClassName: mustUTF16(scanClassName),
		}
		if atom, _, _ := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); atom == 0 {
			return nil
		}
	}

	d := &scanDialog{items: items, scale: owner.scale, hFont: owner.hFont}
	scanDlg = d

	width := int(700 * d.scale)
	height := int(480 * d.scale)
	var orc rect
	pGetWindowRect.Call(owner.hwnd, uintptr(unsafe.Pointer(&orc)))
	x := int(orc.left) + (int(orc.right-orc.left)-width)/2
	y := int(orc.top) + (int(orc.bottom-orc.top)-height)/2
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}

	hwnd, _, _ := pCreateWindowExW.Call(0,
		uintptr(unsafe.Pointer(mustUTF16(scanClassName))),
		uintptr(unsafe.Pointer(mustUTF16("导入扫描结果"))),
		wsCaption|wsSysMenu,
		uintptr(uint32(x)), uintptr(uint32(y)),
		uintptr(uint32(width)), uintptr(uint32(height)),
		owner.hwnd, 0, hInst, 0)
	if hwnd == 0 {
		scanDlg = nil
		return nil
	}
	d.hwnd = hwnd

	d.hList, _, _ = pCreateWindowExW.Call(wsExClientEdge,
		uintptr(unsafe.Pointer(mustUTF16("SysListView32"))),
		0, uintptr(wsChild|wsVisible|wsTabStop|lvsReport|lvsSingleSel|lvsShowSelAlways),
		0, 0, 0, 0, hwnd, scnList, hInst, 0)
	sendMsg(d.hList, wmSetFont, d.hFont, 1)
	ext := uintptr(lvsExCheckBoxes | lvsExFullRowSelect | lvsExDoubleBuffer | lvsExLabelTip)
	sendMsg(d.hList, lvmSetExtStyle, ext, ext)
	for i, name := range []string{"名称", "路径"} {
		col := lvColumnW{
			mask:     lvcfFmt | lvcfWidth | lvcfText,
			fmt:      lvcfmtLeft,
			cx:       int32(180 * d.scale),
			pszText:  mustUTF16(name),
			iSubItem: int32(i),
		}
		sendMsg(d.hList, lvmInsertColumnW, uintptr(i), uintptr(unsafe.Pointer(&col)))
	}
	for i, p := range items {
		lvSetItemText(d.hList, i, 0, exeBaseName(p))
		lvSetItemText(d.hList, i, 1, p)
	}
	d.setAllChecks(true)

	labels := []string{"全选", "全不选", "导入", "取消"}
	ids := []uintptr{scnSelectAll, scnSelectNone, scnImport, scnCancel}
	for i := range labels {
		d.buttons[i], _, _ = pCreateWindowExW.Call(0,
			uintptr(unsafe.Pointer(mustUTF16("BUTTON"))),
			uintptr(unsafe.Pointer(mustUTF16(labels[i]))),
			uintptr(wsChild|wsVisible|wsTabStop),
			0, 0, 0, 0, hwnd, ids[i], hInst, 0)
		sendMsg(d.buttons[i], wmSetFont, d.hFont, 1)
	}

	d.layout()

	pEnableWindow.Call(owner.hwnd, 0)
	pShowWindow.Call(hwnd, swShow)
	pUpdateWindow.Call(hwnd)
	pSetFocus.Call(d.hList)

	// 模态消息循环：对话框销毁或收到 WM_QUIT（主窗口被关）时退出
	var m msg
	quit := false
	for {
		r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) == 0 {
			quit = true
			break
		}
		if int32(r) == -1 {
			break
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
		if alive, _, _ := pIsWindow.Call(hwnd); alive == 0 {
			break
		}
	}

	pEnableWindow.Call(owner.hwnd, 1)
	pSetForegroundWindow.Call(owner.hwnd)
	scanDlg = nil
	if quit {
		pPostQuitMessage.Call(m.wParam)
		return nil
	}
	return d.chosen
}

func scanWndProc(hwnd uintptr, msg uint32, wp, lp uintptr) uintptr {
	defer recoverLog("扫描对话框 wndproc")
	d := scanDlg
	if d == nil || d.hwnd != hwnd {
		r, _, _ := pDefWindowProcW.Call(hwnd, uintptr(msg), wp, lp)
		return r
	}
	switch msg {
	case wmSize:
		d.layout()
		return 0
	case wmCommand:
		switch loword(wp) {
		case scnSelectAll:
			d.setAllChecks(true)
		case scnSelectNone:
			d.setAllChecks(false)
		case scnImport:
			d.chosen = d.checkedItems()
			pDestroyWindow.Call(hwnd)
		case scnCancel:
			pDestroyWindow.Call(hwnd)
		}
		return 0
	case wmClose:
		pDestroyWindow.Call(hwnd)
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, uintptr(msg), wp, lp)
	return r
}

func (d *scanDialog) layout() {
	var rc rect
	pGetClientRect.Call(d.hwnd, uintptr(unsafe.Pointer(&rc)))
	cw := int(rc.right - rc.left)
	ch := int(rc.bottom - rc.top)
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
	pMoveWindow.Call(d.hList, uintptr(margin), uintptr(margin), uintptr(cw-2*margin), uintptr(listH), 1)

	y := ch - bh - pad
	x := cw - margin - bw
	for _, i := range []int{3, 2, 1, 0} { // 取消/导入/全不选/全选 从右往左
		pMoveWindow.Call(d.buttons[i], uintptr(x), uintptr(y), uintptr(bw), uintptr(bh), 1)
		x -= bw + pad
	}
}

// setAllChecks iItem=-1 应用到全部；2<<12=选中，1<<12=未选中。
func (d *scanDialog) setAllChecks(check bool) {
	state := uint32(1 << 12)
	if check {
		state = 2 << 12
	}
	it := lvItemW{state: state, stateMask: lvisStateImageMask, iItem: -1}
	sendMsg(d.hList, lvmSetItemState, ^uintptr(0), uintptr(unsafe.Pointer(&it)))
}

func (d *scanDialog) checkedItems() []string {
	var out []string
	for i := range d.items {
		st := sendMsg(d.hList, lvmGetItemState, uintptr(i), lvisStateImageMask)
		if st&0xF000 == 2<<12 {
			out = append(out, d.items[i])
		}
	}
	return out
}
