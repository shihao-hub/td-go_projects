package ui

import (
	"strings"
	"syscall"
	"unsafe"

	"exe-launcher/internal/model"
	"exe-launcher/internal/win32"
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
func runTagDialog(owner *mainWindow, e *model.Entry) (sysTag, userTag string, ok bool) {
	hInst, _, _ := win32.GetModuleHandleW.Call(0)
	if tagWndProcCb == 0 {
		tagWndProcCb = syscall.NewCallback(tagWndProc)
		wc := win32.WndClassExW{
			CbSize:        uint32(unsafe.Sizeof(win32.WndClassExW{})),
			LpfnWndProc:   tagWndProcCb,
			HInstance:     hInst,
			HCursor:       loadArrowCursor(),
			HbrBackground: win32.COLOR_WINDOW + 1,
			LpszClassName: win32.MustUTF16(tagClassName),
		}
		if atom, _, _ := win32.RegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); atom == 0 {
			return "", "", false
		}
	}

	d := &tagDialog{scale: owner.scale, hFont: owner.hFont}
	tagDlg = d

	width := int(400 * d.scale)
	height := int(200 * d.scale)
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
		uintptr(unsafe.Pointer(win32.MustUTF16(tagClassName))),
		uintptr(unsafe.Pointer(win32.MustUTF16(e.Name+" - 打标"))),
		win32.WS_CAPTION|win32.WS_SYSMENU,
		uintptr(uint32(x)), uintptr(uint32(y)),
		uintptr(uint32(width)), uintptr(uint32(height)),
		owner.hwnd, 0, hInst, 0)
	if hwnd == 0 {
		tagDlg = nil
		return "", "", false
	}
	d.hwnd = hwnd

	mkChild := func(class, text string, style, exStyle uintptr, id int) uintptr {
		h, _, _ := win32.CreateWindowExW.Call(exStyle,
			uintptr(unsafe.Pointer(win32.MustUTF16(class))),
			uintptr(unsafe.Pointer(win32.MustUTF16(text))),
			style, 0, 0, 0, 0, hwnd, uintptr(id), hInst, 0)
		win32.SendMsg(h, win32.WM_SETFONT, d.hFont, 1)
		return h
	}
	child := uintptr(win32.WS_CHILD | win32.WS_VISIBLE | win32.WS_TABSTOP)

	d.labels[0] = mkChild("STATIC", "系统标签：", uintptr(win32.WS_CHILD|win32.WS_VISIBLE), 0, tgStaticSys)
	d.hCombo = mkChild("COMBOBOX", "", child|win32.CBS_DROPDOWNLIST, 0, tgCombo)
	win32.SendMsg(d.hCombo, win32.CB_ADDSTRING, 0, uintptr(unsafe.Pointer(win32.MustUTF16("无"))))
	for _, t := range model.SysTagDefs {
		win32.SendMsg(d.hCombo, win32.CB_ADDSTRING, 0, uintptr(unsafe.Pointer(win32.MustUTF16(t.Label))))
	}
	sel := 0 // 默认"无"；命中当前标签则选中对应项
	for i, t := range model.SysTagDefs {
		if t.Key == e.SysTag {
			sel = i + 1
		}
	}
	win32.SendMsg(d.hCombo, win32.CB_SETCURSEL, uintptr(sel), 0)

	d.labels[1] = mkChild("STATIC", "用户标签：", uintptr(win32.WS_CHILD|win32.WS_VISIBLE), 0, tgStaticUsr)
	d.hEdit = mkChild("EDIT", e.UserTag, child|win32.ES_AUTOHSCROLL, win32.WS_EX_CLIENTEDGE, tgEdit)

	d.buttons[0], _, _ = win32.CreateWindowExW.Call(0,
		uintptr(unsafe.Pointer(win32.MustUTF16("BUTTON"))),
		uintptr(unsafe.Pointer(win32.MustUTF16("确定"))),
		uintptr(win32.WS_CHILD|win32.WS_VISIBLE|win32.WS_TABSTOP|win32.BS_DEFPUSHBUTTON),
		0, 0, 0, 0, hwnd, tgOK, hInst, 0)
	d.buttons[1], _, _ = win32.CreateWindowExW.Call(0,
		uintptr(unsafe.Pointer(win32.MustUTF16("BUTTON"))),
		uintptr(unsafe.Pointer(win32.MustUTF16("取消"))),
		uintptr(win32.WS_CHILD|win32.WS_VISIBLE|win32.WS_TABSTOP),
		0, 0, 0, 0, hwnd, tgCancel, hInst, 0)
	for _, b := range d.buttons {
		win32.SendMsg(b, win32.WM_SETFONT, d.hFont, 1)
	}

	d.layout()

	win32.EnableWindow.Call(owner.hwnd, 0)
	win32.ShowWindow.Call(hwnd, win32.SW_SHOW)
	win32.UpdateWindow.Call(hwnd)
	win32.SetFocus.Call(d.hCombo)

	// 模态消息循环：IsDialogMessage 处理 Tab/Enter/Esc，其余正常分发；
	// 对话框销毁或收到 WM_QUIT（主窗口被关）时退出。
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
		if isDlg, _, _ := win32.IsDialogMessageW.Call(hwnd, uintptr(unsafe.Pointer(&m))); isDlg == 0 {
			win32.TranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
			win32.DispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
		}
		if alive, _, _ := win32.IsWindow.Call(hwnd); alive == 0 {
			break
		}
	}

	win32.EnableWindow.Call(owner.hwnd, 1)
	win32.SetForegroundWindow.Call(owner.hwnd)
	tagDlg = nil
	if quit {
		win32.PostQuitMessage.Call(m.WParam)
		return "", "", false
	}
	return d.sysTag, d.userTag, d.ok
}

func tagWndProc(hwnd uintptr, msg uint32, wp, lp uintptr) uintptr {
	defer recoverLog("打标对话框 wndproc")
	d := tagDlg
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
		case tgOK:
			d.readResults()
			d.ok = true
			win32.DestroyWindow.Call(hwnd)
		case tgCancel, win32.IDCANCEL:
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

// readResults 从控件读回结果：下拉 -1/0 视为无系统标签，用户标签去首尾空白。
func (d *tagDialog) readResults() {
	sel := int(int32(win32.SendMsg(d.hCombo, win32.CB_GETCURSEL, 0, 0)))
	d.sysTag = model.SysTagNone
	if sel >= 1 && sel <= len(model.SysTagDefs) {
		d.sysTag = model.SysTagDefs[sel-1].Key
	}

	n := int(win32.SendMsg(d.hEdit, win32.WM_GETTEXTLENGTH, 0, 0))
	if n <= 0 {
		d.userTag = ""
		return
	}
	buf := make([]uint16, n+1)
	win32.SendMsg(d.hEdit, win32.WM_GETTEXT, uintptr(len(buf)), uintptr(unsafe.Pointer(&buf[0])))
	d.userTag = strings.TrimSpace(win32.UTF16PtrToString(&buf[0]))
}

func (d *tagDialog) layout() {
	var rc win32.Rect
	win32.GetClientRect.Call(d.hwnd, uintptr(unsafe.Pointer(&rc)))
	cw := int(rc.Right - rc.Left)
	ch := int(rc.Bottom - rc.Top)
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
	win32.MoveWindow.Call(d.labels[0], uintptr(margin), uintptr(y), uintptr(labelW), uintptr(rowH), 1)
	// ComboBox 的窗口高度是展开总高度，闭合高度由字体决定，垂直大致对齐标签行即可
	win32.MoveWindow.Call(d.hCombo, uintptr(inputX), uintptr(y-int(3*d.scale)), uintptr(inputW), uintptr(int(200*d.scale)), 1)

	y += rowH + gap
	win32.MoveWindow.Call(d.labels[1], uintptr(margin), uintptr(y), uintptr(labelW), uintptr(rowH), 1)
	win32.MoveWindow.Call(d.hEdit, uintptr(inputX), uintptr(y), uintptr(inputW), uintptr(rowH), 1)

	bw := int(88 * d.scale)
	bh := int(28 * d.scale)
	by := ch - bh - margin
	if by < y+rowH+gap {
		by = y + rowH + gap
	}
	x := cw - margin - bw
	win32.MoveWindow.Call(d.buttons[1], uintptr(x), uintptr(by), uintptr(bw), uintptr(bh), 1)
	x -= bw + int(10*d.scale)
	win32.MoveWindow.Call(d.buttons[0], uintptr(x), uintptr(by), uintptr(bw), uintptr(bh), 1)
}
