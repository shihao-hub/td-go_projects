package main

import (
	"strings"
	"syscall"
	"unsafe"
)

// 打标对话框：系统标签下拉（单选，"无"在最前）+ 用户标签单行输入。
// 模态模式与扫描对话框一致：禁用主窗 + 本地消息循环 + WM_QUIT 透传；
// 额外走 IsDialogMessage，让 Tab 切焦点 / Enter 确定 / Esc 取消生效。

const (
	tgStaticSys = 301
	tgStaticUsr = 302
	tgCombo     = 303
	tgEdit      = 304

	tgOK     = 3001
	tgCancel = 3002
)

const tagClassName = "ExeLauncherTagWnd"

type tagDialog struct {
	hwnd    uintptr
	hCombo  uintptr
	hEdit   uintptr
	labels  [2]uintptr
	buttons [2]uintptr
	sysTag  string
	userTag string
	ok      bool
	scale   float64
	hFont   uintptr
}

var (
	tagDlg       *tagDialog
	tagWndProcCb uintptr
)

// runTagDialog 弹出模态打标对话框；确定返回新的系统/用户标签，取消 ok=false。
func runTagDialog(owner *mainWindow, e *Entry) (sysTag, userTag string, ok bool) {
	hInst, _, _ := pGetModuleHandleW.Call(0)
	if tagWndProcCb == 0 {
		tagWndProcCb = syscall.NewCallback(tagWndProc)
		wc := wndClassExW{
			cbSize:        uint32(unsafe.Sizeof(wndClassExW{})),
			lpfnWndProc:   tagWndProcCb,
			hInstance:     hInst,
			hCursor:       loadArrowCursor(),
			hbrBackground: colorWindow + 1,
			lpszClassName: mustUTF16(tagClassName),
		}
		if atom, _, _ := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); atom == 0 {
			return "", "", false
		}
	}

	d := &tagDialog{scale: owner.scale, hFont: owner.hFont}
	tagDlg = d

	width := int(400 * d.scale)
	height := int(200 * d.scale)
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
		uintptr(unsafe.Pointer(mustUTF16(tagClassName))),
		uintptr(unsafe.Pointer(mustUTF16(e.Name+" - 打标"))),
		wsCaption|wsSysMenu,
		uintptr(uint32(x)), uintptr(uint32(y)),
		uintptr(uint32(width)), uintptr(uint32(height)),
		owner.hwnd, 0, hInst, 0)
	if hwnd == 0 {
		tagDlg = nil
		return "", "", false
	}
	d.hwnd = hwnd

	mkChild := func(class, text string, style, exStyle uintptr, id int) uintptr {
		h, _, _ := pCreateWindowExW.Call(exStyle,
			uintptr(unsafe.Pointer(mustUTF16(class))),
			uintptr(unsafe.Pointer(mustUTF16(text))),
			style, 0, 0, 0, 0, hwnd, uintptr(id), hInst, 0)
		sendMsg(h, wmSetFont, d.hFont, 1)
		return h
	}
	child := uintptr(wsChild | wsVisible | wsTabStop)

	d.labels[0] = mkChild("STATIC", "系统标签：", uintptr(wsChild|wsVisible), 0, tgStaticSys)
	d.hCombo = mkChild("COMBOBOX", "", child|cbsDropdownlist, 0, tgCombo)
	sendMsg(d.hCombo, cbAddString, 0, uintptr(unsafe.Pointer(mustUTF16("无"))))
	for _, t := range sysTagDefs {
		sendMsg(d.hCombo, cbAddString, 0, uintptr(unsafe.Pointer(mustUTF16(t.Label))))
	}
	sel := 0 // 默认"无"；命中当前标签则选中对应项
	for i, t := range sysTagDefs {
		if t.Key == e.SysTag {
			sel = i + 1
		}
	}
	sendMsg(d.hCombo, cbSetCurSel, uintptr(sel), 0)

	d.labels[1] = mkChild("STATIC", "用户标签：", uintptr(wsChild|wsVisible), 0, tgStaticUsr)
	d.hEdit = mkChild("EDIT", e.UserTag, child|esAutoHscroll, wsExClientEdge, tgEdit)

	d.buttons[0], _, _ = pCreateWindowExW.Call(0,
		uintptr(unsafe.Pointer(mustUTF16("BUTTON"))),
		uintptr(unsafe.Pointer(mustUTF16("确定"))),
		uintptr(wsChild|wsVisible|wsTabStop|bsDefpushbutton),
		0, 0, 0, 0, hwnd, tgOK, hInst, 0)
	d.buttons[1], _, _ = pCreateWindowExW.Call(0,
		uintptr(unsafe.Pointer(mustUTF16("BUTTON"))),
		uintptr(unsafe.Pointer(mustUTF16("取消"))),
		uintptr(wsChild|wsVisible|wsTabStop),
		0, 0, 0, 0, hwnd, tgCancel, hInst, 0)
	for _, b := range d.buttons {
		sendMsg(b, wmSetFont, d.hFont, 1)
	}

	d.layout()

	pEnableWindow.Call(owner.hwnd, 0)
	pShowWindow.Call(hwnd, swShow)
	pUpdateWindow.Call(hwnd)
	pSetFocus.Call(d.hCombo)

	// 模态消息循环：IsDialogMessage 处理 Tab/Enter/Esc，其余正常分发；
	// 对话框销毁或收到 WM_QUIT（主窗口被关）时退出。
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
		if isDlg, _, _ := pIsDialogMessageW.Call(hwnd, uintptr(unsafe.Pointer(&m))); isDlg == 0 {
			pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
			pDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
		}
		if alive, _, _ := pIsWindow.Call(hwnd); alive == 0 {
			break
		}
	}

	pEnableWindow.Call(owner.hwnd, 1)
	pSetForegroundWindow.Call(owner.hwnd)
	tagDlg = nil
	if quit {
		pPostQuitMessage.Call(m.wParam)
		return "", "", false
	}
	return d.sysTag, d.userTag, d.ok
}

func tagWndProc(hwnd uintptr, msg uint32, wp, lp uintptr) uintptr {
	defer recoverLog("打标对话框 wndproc")
	d := tagDlg
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
		case tgOK:
			d.readResults()
			d.ok = true
			pDestroyWindow.Call(hwnd)
		case tgCancel, idCancel:
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

// readResults 从控件读回结果：下拉 -1/0 视为无系统标签，用户标签去首尾空白。
func (d *tagDialog) readResults() {
	sel := int(int32(sendMsg(d.hCombo, cbGetCurSel, 0, 0)))
	d.sysTag = sysTagNone
	if sel >= 1 && sel <= len(sysTagDefs) {
		d.sysTag = sysTagDefs[sel-1].Key
	}

	n := int(sendMsg(d.hEdit, wmGettextLength, 0, 0))
	if n <= 0 {
		d.userTag = ""
		return
	}
	buf := make([]uint16, n+1)
	sendMsg(d.hEdit, wmGettext, uintptr(len(buf)), uintptr(unsafe.Pointer(&buf[0])))
	d.userTag = strings.TrimSpace(utf16PtrToString(&buf[0]))
}

func (d *tagDialog) layout() {
	var rc rect
	pGetClientRect.Call(d.hwnd, uintptr(unsafe.Pointer(&rc)))
	cw := int(rc.right - rc.left)
	ch := int(rc.bottom - rc.top)
	if cw <= 0 || ch <= 0 {
		return
	}
	margin := int(14 * d.scale)
	gap := int(12 * d.scale)
	labelW := int(76 * d.scale)
	rowH := int(26 * d.scale)
	inputX := margin + labelW
	inputW := cw - inputX - margin
	if inputW < 80 {
		inputW = 80
	}

	y := margin
	pMoveWindow.Call(d.labels[0], uintptr(margin), uintptr(y), uintptr(labelW), uintptr(rowH), 1)
	// ComboBox 的窗口高度是展开总高度，闭合高度由字体决定，垂直大致对齐标签行即可
	pMoveWindow.Call(d.hCombo, uintptr(inputX), uintptr(y-int(3*d.scale)), uintptr(inputW), uintptr(int(200*d.scale)), 1)

	y += rowH + gap
	pMoveWindow.Call(d.labels[1], uintptr(margin), uintptr(y), uintptr(labelW), uintptr(rowH), 1)
	pMoveWindow.Call(d.hEdit, uintptr(inputX), uintptr(y), uintptr(inputW), uintptr(rowH), 1)

	bw := int(88 * d.scale)
	bh := int(28 * d.scale)
	by := ch - bh - margin
	if by < y+rowH+gap {
		by = y + rowH + gap
	}
	x := cw - margin - bw
	pMoveWindow.Call(d.buttons[1], uintptr(x), uintptr(by), uintptr(bw), uintptr(bh), 1)
	x -= bw + int(10*d.scale)
	pMoveWindow.Call(d.buttons[0], uintptr(x), uintptr(by), uintptr(bw), uintptr(bh), 1)
}
