package main

import (
	"fmt"
	"log"
	"path/filepath"
	"syscall"
	"unsafe"
)

// 命令 ID：工具栏与右键菜单共用同一套 WM_COMMAND 分支
const (
	idcAddExe  = 1001
	idcScanDir = 1002
	idcLaunch  = 1003
	idcOpenDir = 1004
	idcPowerSh = 1005
	idcRemove  = 1006
	idcClean   = 1007
	idcRefresh = 1008
	idcMdDoc   = 1009
)

// 控件 ID（WM_NOTIFY 里用 idFrom 区分来源）
const (
	idList    = 100
	idToolbar = 101
	idStatus  = 102
)

const (
	mainClassName = "ExeLauncherWnd"
	mainTitle     = "EXE 启动器"
)

type mainWindow struct {
	hwnd     uintptr
	hToolbar uintptr
	hList    uintptr
	hStatus  uintptr
	hFont    uintptr
	tbH      int
	scale    float64
	st       *store
	cfg      *config
}

var mainWin *mainWindow

var mainWndProcCb = syscall.NewCallback(func(hwnd uintptr, msg uint32, wp, lp uintptr) uintptr {
	defer recoverLog("主窗口 wndproc")
	w := mainWin
	if w == nil || w.hwnd != hwnd {
		r, _, _ := pDefWindowProcW.Call(hwnd, uintptr(msg), wp, lp)
		return r
	}
	// TaskbarCreated 是 RegisterWindowMessage 动态值，须在固定 switch 外比较
	if taskbarCreatedMsg != 0 && msg == taskbarCreatedMsg {
		trayReAdd(hwnd)
	}
	switch msg {
	case wmAppTray:
		onTrayMessage(w, lp)
		return 0
	case wmClose:
		// 关闭 = 隐藏到托盘，真正退出走托盘菜单「退出」
		pShowWindow.Call(hwnd, swHide)
		return 0
	case wmSize:
		w.layout()
		return 0
	case wmCommand:
		id := loword(wp)
		log.Printf("WM_COMMAND id=%d", id)
		w.onCommand(id)
		return 0
	case wmNotify:
		return w.onNotify(lp)
	case wmDpiChanged:
		w.onDpiChanged(wp, lp)
		return 0
	case wmDestroy:
		trayRemove(hwnd)
		pPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, uintptr(msg), wp, lp)
	return r
})

func loadArrowCursor() uintptr {
	h, _, _ := pLoadCursorW.Call(0, idcArrow)
	return h
}

func systemScale() float64 {
	hdc, _, _ := pGetDC.Call(0)
	if hdc == 0 {
		return 1
	}
	defer pReleaseDC.Call(0, hdc)
	dpi, _, _ := pGetDeviceCaps.Call(hdc, logPixelSX)
	if dpi < 96 {
		return 1
	}
	return float64(dpi) / 96
}

func windowDPI(hwnd uintptr) uint32 {
	r, _, _ := pGetDpiForWindow.Call(hwnd)
	if r == 0 || r < 96 {
		return 96
	}
	return uint32(r)
}

func createMainWindow(st *store, cfg *config) (*mainWindow, error) {
	hInst, _, _ := pGetModuleHandleW.Call(0)

	// 资源 ID 1 的图标组（winres.json RT_GROUP_ICON "#1"），窗口左上角/任务栏用
	hIcon, _, _ := pLoadIconW.Call(hInst, 1)
	log.Printf("图标加载: hIcon=0x%X", hIcon)

	wc := wndClassExW{
		cbSize:        uint32(unsafe.Sizeof(wndClassExW{})),
		lpfnWndProc:   mainWndProcCb,
		hInstance:     hInst,
		hCursor:       loadArrowCursor(),
		hIcon:         hIcon,
		hIconSm:       hIcon,
		hbrBackground: colorWindow + 1,
		lpszClassName: mustUTF16(mainClassName),
	}
	atom, _, _ := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if atom == 0 {
		return nil, fmt.Errorf("RegisterClassExW 失败")
	}

	w := &mainWindow{st: st, cfg: cfg, scale: systemScale()}
	mainWin = w

	hwnd, _, err := pCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(mustUTF16(mainClassName))),
		uintptr(unsafe.Pointer(mustUTF16(mainTitle))),
		wsOverlappedWindow,
		uintptr(cwUseDefault), uintptr(cwUseDefault),
		uintptr(uint32(int32(980*w.scale))), uintptr(uint32(int32(620*w.scale))),
		0, 0, hInst, 0,
	)
	if hwnd == 0 {
		mainWin = nil
		return nil, fmt.Errorf("CreateWindowExW 失败: %v", err)
	}
	w.hwnd = hwnd
	// 显式设到窗口上：标题栏/任务栏直接可用，外部程序 WM_GETICON 也能查到
	sendMsg(hwnd, wmSetIcon, 1, hIcon) // ICON_BIG
	sendMsg(hwnd, wmSetIcon, 0, hIcon) // ICON_SMALL

	if dpi := windowDPI(hwnd); dpi != 96 {
		w.scale = float64(dpi) / 96
	}

	w.hFont = w.createFont()
	w.createToolbar(hInst)
	w.createList(hInst)
	w.createStatus(hInst)
	w.reloadList()
	w.layout()
	pShowWindow.Call(hwnd, swShow)
	pUpdateWindow.Call(hwnd)
	pSetFocus.Call(w.hList)
	return w, nil
}

func (w *mainWindow) createFont() uintptr {
	height := int32(9 * w.scale * 96 / 72) // 9pt
	f, _, _ := pCreateFontW.Call(
		uintptr(-height), 0, 0, 0, fwNormal,
		0, 0, 0, defaultCharset,
		outDefaultPrecis, clipDefaultPrecis, clearTypeQuality, defaultPitch,
		uintptr(unsafe.Pointer(mustUTF16("Microsoft YaHei UI"))),
	)
	return f
}

func (w *mainWindow) createToolbar(hInst uintptr) {
	// 注意不能带 CCS_NORESIZE/CCS_NOPARENTALIGN：以 0x0 创建后 TB_AUTOSIZE 就撑不开，
	// 工具栏会保持 0 高度而不可见。
	style := uintptr(wsChild | wsVisible | wsClipSiblings | tbstyFlat | ccsNoDivider)
	w.hToolbar, _, _ = pCreateWindowExW.Call(0,
		uintptr(unsafe.Pointer(mustUTF16("ToolbarWindow32"))),
		0, style, 0, 0, 100, 30, w.hwnd, idToolbar, hInst, 0)
	sendMsg(w.hToolbar, wmSetFont, w.hFont, 1)
	sendMsg(w.hToolbar, tbButtonStructSize, unsafe.Sizeof(tbButton{}), 0)

	strs := utf16DoubleNull("添加 EXE", "扫描目录", "启动", "打开目录", "PowerShell", "介绍", "移除", "清理失效", "刷新")
	base := sendMsg(w.hToolbar, tbAddString, 0, uintptr(unsafe.Pointer(&strs[0])))

	mk := func(id int, s uintptr) tbButton {
		return tbButton{
			iBitmap:   iImagenameNone,
			idCommand: int32(id),
			fsState:   tbstateEnabled,
			fsStyle:   tbstyButton | tbstyAutosize,
			iString:   s,
		}
	}
	btns := []tbButton{
		mk(idcAddExe, base+0),
		mk(idcScanDir, base+1),
		{fsStyle: tbstySep},
		mk(idcLaunch, base+2),
		mk(idcOpenDir, base+3),
		mk(idcPowerSh, base+4),
		mk(idcMdDoc, base+5),
		{fsStyle: tbstySep},
		mk(idcRemove, base+6),
		mk(idcClean, base+7),
		mk(idcRefresh, base+8),
	}
	sendMsg(w.hToolbar, tbAddButtons, uintptr(len(btns)), uintptr(unsafe.Pointer(&btns[0])))
	sendMsg(w.hToolbar, tbAutosize, 0, 0)
	w.tbH = clientHeight(w.hToolbar)
	if w.tbH <= 0 {
		w.tbH = int(30 * w.scale) // 兜底，避免量出 0 把工具栏压没
	}
}

func (w *mainWindow) createList(hInst uintptr) {
	style := uintptr(wsChild | wsVisible | wsClipSiblings | wsTabStop | lvsReport | lvsSingleSel | lvsShowSelAlways)
	w.hList, _, _ = pCreateWindowExW.Call(wsExClientEdge,
		uintptr(unsafe.Pointer(mustUTF16("SysListView32"))),
		0, style, 0, 0, 0, 0, w.hwnd, idList, hInst, 0)
	sendMsg(w.hList, wmSetFont, w.hFont, 1)
	ext := uintptr(lvsExFullRowSelect | lvsExDoubleBuffer | lvsExLabelTip)
	sendMsg(w.hList, lvmSetExtStyle, ext, ext)

	for i, name := range []string{"名称", "路径", "状态"} {
		col := lvColumnW{
			mask:     lvcfFmt | lvcfWidth | lvcfText,
			fmt:      lvcfmtLeft,
			cx:       int32(160 * w.scale),
			pszText:  mustUTF16(name),
			iSubItem: int32(i),
		}
		sendMsg(w.hList, lvmInsertColumnW, uintptr(i), uintptr(unsafe.Pointer(&col)))
	}
}

func (w *mainWindow) createStatus(hInst uintptr) {
	w.hStatus, _, _ = pCreateWindowExW.Call(0,
		uintptr(unsafe.Pointer(mustUTF16("msctls_statusbar32"))),
		0, uintptr(wsChild|wsVisible|wsClipSiblings), 0, 0, 0, 0, w.hwnd, idStatus, hInst, 0)
	sendMsg(w.hStatus, wmSetFont, w.hFont, 1)
}

func (w *mainWindow) layout() {
	var rc rect
	pGetClientRect.Call(w.hwnd, uintptr(unsafe.Pointer(&rc)))
	cw := int(rc.right - rc.left)
	ch := int(rc.bottom - rc.top)
	if cw <= 0 || ch <= 0 {
		return
	}

	if w.tbH <= 0 {
		w.tbH = clientHeight(w.hToolbar)
		if w.tbH <= 0 {
			w.tbH = int(30 * w.scale)
		}
	}
	pMoveWindow.Call(w.hToolbar, 0, 0, uintptr(cw), uintptr(w.tbH), 1)

	sendMsg(w.hStatus, wmSize, 0, makelparam(cw, ch))
	sbH := clientHeight(w.hStatus)

	listH := ch - w.tbH - sbH
	if listH < 40 {
		listH = 40
	}
	pMoveWindow.Call(w.hList, 0, uintptr(w.tbH), uintptr(cw), uintptr(listH), 1)

	var lrc rect
	pGetClientRect.Call(w.hList, uintptr(unsafe.Pointer(&lrc)))
	vsb, _, _ := pGetSystemMetrics.Call(smCxVScroll)
	colName := int(220 * w.scale)
	colState := int(70 * w.scale)
	colPath := int(lrc.right) - colName - colState - int(vsb) - 4
	if colPath < 120 {
		colPath = 120
	}
	sendMsg(w.hList, lvmSetColumnWidth, 0, uintptr(colName))
	sendMsg(w.hList, lvmSetColumnWidth, 1, uintptr(colPath))
	sendMsg(w.hList, lvmSetColumnWidth, 2, uintptr(colState))
}

func (w *mainWindow) onDpiChanged(wp, lp uintptr) {
	dpi := hiword(wp)
	if dpi < 96 {
		return
	}
	ns := float64(dpi) / 96
	if ns == w.scale {
		return
	}
	w.scale = ns
	pDeleteObject.Call(w.hFont)
	w.hFont = w.createFont()
	for _, h := range []uintptr{w.hToolbar, w.hList, w.hStatus} {
		sendMsg(h, wmSetFont, w.hFont, 1)
	}
	sendMsg(w.hToolbar, tbAutosize, 0, 0)
	w.tbH = clientHeight(w.hToolbar)

	rc := lparamCopy[rect](lp)
	pSetWindowPos.Call(w.hwnd, 0,
		uintptr(rc.left), uintptr(rc.top),
		uintptr(rc.right-rc.left), uintptr(rc.bottom-rc.top),
		swpNoZOrder)
	w.layout()
}

func statusText(valid bool) string {
	if valid {
		return "正常"
	}
	return "失效"
}

func (w *mainWindow) reloadList() {
	sendMsg(w.hList, wmSetRedraw, 0, 0)
	sendMsg(w.hList, lvmDeleteAllItems, 0, 0)
	for i, e := range w.st.entries {
		lvSetItemText(w.hList, i, 0, e.Name)
		lvSetItemText(w.hList, i, 1, e.Path)
		lvSetItemText(w.hList, i, 2, statusText(e.Valid))
	}
	sendMsg(w.hList, wmSetRedraw, 1, 0)
	w.updateStatus()
	w.updateButtonStates()
}

func (w *mainWindow) updateStatus() {
	text := fmt.Sprintf("共 %d 项，%d 项失效", len(w.st.entries), w.st.InvalidCount())
	sendMsg(w.hStatus, sbSetText, 0, uintptr(unsafe.Pointer(mustUTF16(text))))
}

// updateButtonStates 单选一行后 启动/打开目录/PowerShell/移除 才可用。
func (w *mainWindow) updateButtonStates() {
	enable := lvSelected(w.hList) >= 0
	var flag uintptr
	if enable {
		flag = 1
	}
	for _, id := range []int{idcLaunch, idcOpenDir, idcPowerSh, idcMdDoc, idcRemove} {
		sendMsg(w.hToolbar, tbEnableButton, uintptr(id), flag)
	}
}

func (w *mainWindow) save() {
	w.cfg.Entries = w.st.Snapshot()
	if err := saveConfig(w.cfg); err != nil {
		msgBox(w.hwnd, "保存配置失败", err.Error(), mbIconError)
	}
}

func (w *mainWindow) onCommand(id int) {
	switch id {
	case idcAddExe:
		w.addExeByPicker()
	case idcScanDir:
		w.scanAndImport()
	case idcLaunch:
		w.launchSelected()
	case idcOpenDir:
		w.revealSelected()
	case idcPowerSh:
		w.shellSelected()
	case idcMdDoc:
		w.showMdDoc()
	case idcRemove:
		w.removeSelected()
	case idcClean:
		w.cleanInvalid()
	case idcRefresh:
		w.st.RefreshValid()
		w.reloadList()
	case idTrayShow:
		w.showFromTray()
	case idTrayQuit:
		pDestroyWindow.Call(w.hwnd)
	}
}

func (w *mainWindow) selectedEntry() (int, *Entry) {
	sel := lvSelected(w.hList)
	if sel < 0 || sel >= len(w.st.entries) {
		return -1, nil
	}
	return sel, &w.st.entries[sel]
}

// ensureEntryValid 启动/打开前再校验一次，列表是旧状态时不至于启动到不存在的文件。
func (w *mainWindow) ensureEntryValid(e *Entry) bool {
	if fileExists(e.Path) {
		return true
	}
	msgBox(w.hwnd, "文件不存在", "该文件已失效：\n"+e.Path, mbIconWarn)
	w.st.RefreshValid()
	w.reloadList()
	return false
}

func (w *mainWindow) addExeByPicker() {
	path, err := pickExeFile(w.hwnd, w.defaultPickDir())
	if err != nil {
		msgBox(w.hwnd, "打开文件选择框失败", err.Error(), mbIconError)
		return
	}
	if path == "" {
		return
	}
	if w.st.Add("", path) {
		w.save()
		w.reloadList()
	} else {
		msgBox(w.hwnd, "已存在", "该 EXE 已在列表中：\n"+path, mbIconInfo)
	}
}

func (w *mainWindow) defaultPickDir() string {
	if w.cfg.LastScanDir != "" {
		return w.cfg.LastScanDir
	}
	if len(w.st.entries) > 0 {
		return filepath.Dir(w.st.entries[0].Path)
	}
	return ""
}

func (w *mainWindow) scanAndImport() {
	root, err := pickScanRoot(w.hwnd, w.cfg.LastScanDir)
	if err != nil {
		msgBox(w.hwnd, "打开目录选择框失败", err.Error(), mbIconError)
		return
	}
	if root == "" {
		return
	}
	w.cfg.LastScanDir = root

	results, err := scanDirExe(root)
	if err != nil {
		msgBox(w.hwnd, "扫描失败", err.Error(), mbIconError)
		w.save()
		return
	}
	if len(results) == 0 {
		msgBox(w.hwnd, "扫描完成", "未在该目录找到任何 exe。", mbIconInfo)
		w.save()
		return
	}

	chosen := runScanDialog(w, results)
	for _, p := range chosen {
		w.st.Add("", p)
	}
	w.save()
	w.reloadList()
}

func (w *mainWindow) launchSelected() {
	_, e := w.selectedEntry()
	if e == nil {
		return
	}
	if !w.ensureEntryValid(e) {
		return
	}
	if err := launchExe(e.Path); err != nil {
		msgBox(w.hwnd, "启动失败", err.Error(), mbIconError)
	}
}

func (w *mainWindow) revealSelected() {
	_, e := w.selectedEntry()
	if e == nil {
		return
	}
	if !w.ensureEntryValid(e) {
		return
	}
	if err := revealInExplorer(e.Path); err != nil {
		msgBox(w.hwnd, "打开目录失败", err.Error(), mbIconError)
	}
}

func (w *mainWindow) shellSelected() {
	_, e := w.selectedEntry()
	if e == nil {
		return
	}
	if !w.ensureEntryValid(e) {
		return
	}
	if err := openPowerShell(filepath.Dir(e.Path)); err != nil {
		msgBox(w.hwnd, "打开 PowerShell 失败", err.Error(), mbIconError)
	}
}

func (w *mainWindow) removeSelected() {
	sel, _ := w.selectedEntry()
	if sel < 0 {
		return
	}
	w.st.Remove(sel)
	w.save()
	w.reloadList()
}

func (w *mainWindow) cleanInvalid() {
	w.st.RemoveInvalid()
	w.save()
	w.reloadList()
}

func (w *mainWindow) onNotify(lp uintptr) uintptr {
	hdr := lparamCopy[nmHdr](lp)
	if hdr.idFrom != idList {
		return 0
	}
	switch hdr.code {
	case lvnItemChanged:
		nmlv := lparamCopy[nmListView](lp)
		if nmlv.uChanged&lvifState != 0 &&
			(nmlv.uNewState&lvisSelected) != (nmlv.uOldState&lvisSelected) {
			w.updateButtonStates()
		}
	case nmDblclk:
		w.launchSelected()
	case nmRclick:
		w.showContextMenu()
		return 1
	case nmCustomDraw:
		return w.customDraw(lp)
	}
	return 0
}

// customDraw 失效行整行红字。必须用指针而非拷贝：要写回 clrText 给控件读。
func (w *mainWindow) customDraw(lp uintptr) uintptr {
	nm := (*nmlvCustomDraw)(ptrFromLparam(lp))
	switch nm.nmcd.dwDrawStage {
	case cddsPrepaint:
		return cdrfNotifyItemDraw
	case cddsItemPrepaint:
		idx := int(nm.nmcd.dwItemSpec)
		if idx >= 0 && idx < len(w.st.entries) && !w.st.entries[idx].Valid {
			nm.clrText = rgb(0xC0, 0x30, 0x30)
		}
	}
	return cdrfDoDefault
}

// showContextMenu 右键菜单命令 ID 与工具栏复用，同一套 WM_COMMAND 分支。
func (w *mainWindow) showContextMenu() {
	menu, _, _ := pCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer pDestroyMenu.Call(menu)

	enable := lvSelected(w.hList) >= 0
	appendMenu := func(text string, id int, enabled bool) {
		flags := uintptr(mfString)
		if !enabled {
			flags |= mfGrayed
		}
		pAppendMenuW.Call(menu, flags, uintptr(id), uintptr(unsafe.Pointer(mustUTF16(text))))
	}
	appendMenu("启动", idcLaunch, enable)
	appendMenu("打开目录", idcOpenDir, enable)
	appendMenu("在此目录开 PowerShell", idcPowerSh, enable)
	appendMenu("查看介绍", idcMdDoc, enable)
	pAppendMenuW.Call(menu, mfSeparator, 0, 0)
	appendMenu("移除", idcRemove, enable)

	var pt point
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	pSetForegroundWindow.Call(w.hwnd)
	pTrackPopupMenu.Call(menu, tpmRightButton,
		uintptr(uint32(pt.x)), uintptr(uint32(pt.y)), 0, w.hwnd, 0)
	pPostMessageW.Call(w.hwnd, wmNull, 0, 0)
}
