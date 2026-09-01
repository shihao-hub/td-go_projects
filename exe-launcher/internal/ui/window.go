package ui

import (
	"fmt"
	"log"
	"path/filepath"
	"syscall"
	"unsafe"

	"exe-launcher/internal/model"
	"exe-launcher/internal/scan"
	"exe-launcher/internal/win32"
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
	idcTag     = 1010
)

// 控件 ID（WM_NOTIFY 里用 idFrom 区分来源）
const (
	idList    = 100
	idToolbar = 101
	idStatus  = 102
	idFilter  = 103
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
	hFilter  uintptr
	hFont    uintptr
	tbH      int
	scale    float64
	st       *model.Store
	cfg      *model.Config
	filter   string // 系统标签 key，空 = 不筛选
	visible  []int  // 列表行 → Store.Entries 下标（筛选时下标不连续）
}

var mainWin *mainWindow

var mainWndProcCb = syscall.NewCallback(func(hwnd uintptr, msg uint32, wp, lp uintptr) uintptr {
	defer recoverLog("主窗口 wndproc")
	w := mainWin
	if w == nil || w.hwnd != hwnd {
		r, _, _ := win32.DefWindowProcW.Call(hwnd, uintptr(msg), wp, lp)
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
	case win32.WM_CLOSE:
		// 关闭 = 隐藏到托盘，真正退出走托盘菜单「退出」
		win32.ShowWindow.Call(hwnd, win32.SW_HIDE)
		return 0
	case win32.WM_SIZE:
		w.layout()
		return 0
	case win32.WM_COMMAND:
		id := win32.Loword(wp)
		log.Printf("WM_COMMAND id=%d code=%d", id, win32.Hiword(wp))
		w.onCommand(id, win32.Hiword(wp))
		return 0
	case win32.WM_NOTIFY:
		return w.onNotify(lp)
	case win32.WM_DPICHANGED:
		w.onDpiChanged(wp, lp)
		return 0
	case win32.WM_DESTROY:
		trayRemove(hwnd)
		win32.PostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := win32.DefWindowProcW.Call(hwnd, uintptr(msg), wp, lp)
	return r
})

func loadArrowCursor() uintptr {
	h, _, _ := win32.LoadCursorW.Call(0, win32.IDC_ARROW)
	return h
}

func systemScale() float64 {
	hdc, _, _ := win32.GetDC.Call(0)
	if hdc == 0 {
		return 1
	}
	defer win32.ReleaseDC.Call(0, hdc)
	dpi, _, _ := win32.GetDeviceCaps.Call(hdc, win32.LOGPIXELSX)
	if dpi < 96 {
		return 1
	}
	return float64(dpi) / 96
}

func windowDPI(hwnd uintptr) uint32 {
	r, _, _ := win32.GetDpiForWindow.Call(hwnd)
	if r == 0 || r < 96 {
		return 96
	}
	return uint32(r)
}

func createMainWindow(st *model.Store, cfg *model.Config) (*mainWindow, error) {
	hInst, _, _ := win32.GetModuleHandleW.Call(0)

	// 资源 ID 1 的图标组（winres.json RT_GROUP_ICON "#1"），窗口左上角/任务栏用
	hIcon, _, _ := win32.LoadIconW.Call(hInst, 1)
	log.Printf("图标加载: hIcon=0x%X", hIcon)

	wc := win32.WndClassExW{
		CbSize:        uint32(unsafe.Sizeof(win32.WndClassExW{})),
		LpfnWndProc:   mainWndProcCb,
		HInstance:     hInst,
		HCursor:       loadArrowCursor(),
		HIcon:         hIcon,
		HIconSm:       hIcon,
		HbrBackground: win32.COLOR_WINDOW + 1,
		LpszClassName: win32.MustUTF16(mainClassName),
	}
	atom, _, _ := win32.RegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if atom == 0 {
		return nil, fmt.Errorf("RegisterClassExW 失败")
	}

	w := &mainWindow{st: st, cfg: cfg, scale: systemScale()}
	mainWin = w

	hwnd, _, err := win32.CreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(win32.MustUTF16(mainClassName))),
		uintptr(unsafe.Pointer(win32.MustUTF16(mainTitle))),
		win32.WS_OVERLAPPEDWINDOW,
		uintptr(win32.CW_USEDEFAULT), uintptr(win32.CW_USEDEFAULT),
		uintptr(uint32(int32(980*w.scale))), uintptr(uint32(int32(620*w.scale))),
		0, 0, hInst, 0,
	)
	if hwnd == 0 {
		mainWin = nil
		return nil, fmt.Errorf("CreateWindowExW 失败: %v", err)
	}
	w.hwnd = hwnd
	// 显式设到窗口上：标题栏/任务栏直接可用，外部程序 WM_GETICON 也能查到
	win32.SendMsg(hwnd, win32.WM_SETICON, 1, hIcon) // ICON_BIG
	win32.SendMsg(hwnd, win32.WM_SETICON, 0, hIcon) // ICON_SMALL

	if dpi := windowDPI(hwnd); dpi != 96 {
		w.scale = float64(dpi) / 96
	}

	w.hFont = w.createFont()
	w.createToolbar(hInst)
	w.createFilter(hInst)
	w.createList(hInst)
	w.createStatus(hInst)
	w.reloadList()
	w.layout()
	win32.ShowWindow.Call(hwnd, win32.SW_SHOW)
	win32.UpdateWindow.Call(hwnd)
	win32.SetFocus.Call(w.hList)
	return w, nil
}

func (w *mainWindow) createFont() uintptr {
	height := int32(9 * w.scale * 96 / 72) // 9pt
	f, _, _ := win32.CreateFontW.Call(
		uintptr(-height), 0, 0, 0, win32.FW_NORMAL,
		0, 0, 0, win32.DEFAULT_CHARSET,
		win32.OUT_DEFAULT_PRECIS, win32.CLIP_DEFAULT_PRECIS, win32.CLEARTYPE_QUALITY, win32.DEFAULT_PITCH,
		uintptr(unsafe.Pointer(win32.MustUTF16("Microsoft YaHei UI"))),
	)
	return f
}

func (w *mainWindow) createToolbar(hInst uintptr) {
	// 注意不能带 CCS_NORESIZE/CCS_NOPARENTALIGN：以 0x0 创建后 TB_AUTOSIZE 就撑不开，
	// 工具栏会保持 0 高度而不可见。
	style := uintptr(win32.WS_CHILD | win32.WS_VISIBLE | win32.WS_CLIPSIBLINGS | win32.TBSTYLE_FLAT | win32.CCS_NODIVIDER)
	w.hToolbar, _, _ = win32.CreateWindowExW.Call(0,
		uintptr(unsafe.Pointer(win32.MustUTF16("ToolbarWindow32"))),
		0, style, 0, 0, 100, 30, w.hwnd, idToolbar, hInst, 0)
	win32.SendMsg(w.hToolbar, win32.WM_SETFONT, w.hFont, 1)
	win32.SendMsg(w.hToolbar, win32.TB_BUTTONSTRUCTSIZE, unsafe.Sizeof(win32.TBButton{}), 0)

	strs := win32.UTF16DoubleNull("添加 EXE", "扫描目录", "启动", "打开目录", "PowerShell", "介绍", "打标", "移除", "清理失效", "刷新")
	base := win32.SendMsg(w.hToolbar, win32.TB_ADDSTRING, 0, uintptr(unsafe.Pointer(&strs[0])))

	mk := func(id int, s uintptr) win32.TBButton {
		return win32.TBButton{
			IBitmap:   win32.I_IMAGENAME_NONE,
			IdCommand: int32(id),
			FsState:   win32.TBSTATE_ENABLED,
			FsStyle:   win32.TBSTYLE_BUTTON | win32.TBSTYLE_AUTOSIZE,
			IString:   s,
		}
	}
	btns := []win32.TBButton{
		mk(idcAddExe, base+0),
		mk(idcScanDir, base+1),
		{FsStyle: win32.TBSTYLE_SEP},
		mk(idcLaunch, base+2),
		mk(idcOpenDir, base+3),
		mk(idcPowerSh, base+4),
		mk(idcMdDoc, base+5),
		mk(idcTag, base+6),
		{FsStyle: win32.TBSTYLE_SEP},
		mk(idcRemove, base+7),
		mk(idcClean, base+8),
		mk(idcRefresh, base+9),
	}
	win32.SendMsg(w.hToolbar, win32.TB_ADDBUTTONS, uintptr(len(btns)), uintptr(unsafe.Pointer(&btns[0])))
	win32.SendMsg(w.hToolbar, win32.TB_AUTOSIZE, 0, 0)
	w.tbH = win32.ClientHeight(w.hToolbar)
	if w.tbH <= 0 {
		w.tbH = int(30 * w.scale) // 兜底，避免量出 0 把工具栏压没
	}
}

// createFilter 工具栏行右侧的系统标签筛选下拉（todo 视图：只看某标签的记录）。
func (w *mainWindow) createFilter(hInst uintptr) {
	style := uintptr(win32.WS_CHILD | win32.WS_VISIBLE | win32.WS_TABSTOP | win32.CBS_DROPDOWNLIST)
	w.hFilter, _, _ = win32.CreateWindowExW.Call(0,
		uintptr(unsafe.Pointer(win32.MustUTF16("COMBOBOX"))),
		0, style, 0, 0, 0, 0, w.hwnd, idFilter, hInst, 0)
	win32.SendMsg(w.hFilter, win32.WM_SETFONT, w.hFont, 1)
	win32.SendMsg(w.hFilter, win32.CB_ADDSTRING, 0, uintptr(unsafe.Pointer(win32.MustUTF16("全部"))))
	for _, t := range model.SysTagDefs {
		win32.SendMsg(w.hFilter, win32.CB_ADDSTRING, 0, uintptr(unsafe.Pointer(win32.MustUTF16("只看"+t.Label))))
	}
	win32.SendMsg(w.hFilter, win32.CB_SETCURSEL, 0, 0)
}

func (w *mainWindow) createList(hInst uintptr) {
	style := uintptr(win32.WS_CHILD | win32.WS_VISIBLE | win32.WS_CLIPSIBLINGS | win32.WS_TABSTOP | win32.LVS_REPORT | win32.LVS_SINGLESEL | win32.LVS_SHOWSELALWAYS)
	w.hList, _, _ = win32.CreateWindowExW.Call(win32.WS_EX_CLIENTEDGE,
		uintptr(unsafe.Pointer(win32.MustUTF16("SysListView32"))),
		0, style, 0, 0, 0, 0, w.hwnd, idList, hInst, 0)
	win32.SendMsg(w.hList, win32.WM_SETFONT, w.hFont, 1)
	ext := uintptr(win32.LVS_EX_FULLROWSELECT | win32.LVS_EX_DOUBLEBUFFER | win32.LVS_EX_LABELTIP)
	win32.SendMsg(w.hList, win32.LVM_SETEXTENDEDLISTVIEWSTYLE, ext, ext)

	for i, name := range []string{"名称", "标签", "路径", "状态"} {
		col := win32.LVColumnW{
			Mask:     win32.LVCF_FMT | win32.LVCF_WIDTH | win32.LVCF_TEXT,
			Fmt:      win32.LVCFMT_LEFT,
			Cx:       int32(160 * w.scale),
			PszText:  win32.MustUTF16(name),
			ISubItem: int32(i),
		}
		win32.SendMsg(w.hList, win32.LVM_INSERTCOLUMNW, uintptr(i), uintptr(unsafe.Pointer(&col)))
	}
}

func (w *mainWindow) createStatus(hInst uintptr) {
	w.hStatus, _, _ = win32.CreateWindowExW.Call(0,
		uintptr(unsafe.Pointer(win32.MustUTF16("msctls_statusbar32"))),
		0, uintptr(win32.WS_CHILD|win32.WS_VISIBLE|win32.WS_CLIPSIBLINGS), 0, 0, 0, 0, w.hwnd, idStatus, hInst, 0)
	win32.SendMsg(w.hStatus, win32.WM_SETFONT, w.hFont, 1)
}

func (w *mainWindow) layout() {
	var rc win32.Rect
	win32.GetClientRect.Call(w.hwnd, uintptr(unsafe.Pointer(&rc)))
	cw := int(rc.Right - rc.Left)
	ch := int(rc.Bottom - rc.Top)
	if cw <= 0 || ch <= 0 {
		return
	}

	if w.tbH <= 0 {
		w.tbH = win32.ClientHeight(w.hToolbar)
		if w.tbH <= 0 {
			w.tbH = int(30 * w.scale)
		}
	}
	// 工具栏右侧留出筛选下拉的位置
	filterW := int(120 * w.scale)
	filterGap := int(10 * w.scale)
	win32.MoveWindow.Call(w.hToolbar, 0, 0, uintptr(cw-filterW-filterGap-4), uintptr(w.tbH), 1)

	// ComboBox 窗口高度是展开总高度，闭合高度由字体决定；垂直居中于工具栏行
	fh := int(200 * w.scale)
	fClosed := win32.ClientHeight(w.hFilter)
	if fClosed <= 0 {
		fClosed = int(22 * w.scale)
	}
	fy := (w.tbH - fClosed) / 2
	if fy < 0 {
		fy = 0
	}
	win32.MoveWindow.Call(w.hFilter, uintptr(cw-filterW), uintptr(fy), uintptr(filterW), uintptr(fh), 1)

	win32.SendMsg(w.hStatus, win32.WM_SIZE, 0, win32.Makelparam(cw, ch))
	sbH := win32.ClientHeight(w.hStatus)

	listH := ch - w.tbH - sbH
	if listH < 40 {
		listH = 40
	}
	win32.MoveWindow.Call(w.hList, 0, uintptr(w.tbH), uintptr(cw), uintptr(listH), 1)

	var lrc win32.Rect
	win32.GetClientRect.Call(w.hList, uintptr(unsafe.Pointer(&lrc)))
	vsb, _, _ := win32.GetSystemMetrics.Call(win32.SM_CXVSCROLL)
	colName := int(200 * w.scale)
	colTag := int(150 * w.scale)
	colState := int(64 * w.scale)
	colPath := int(lrc.Right) - colName - colTag - colState - int(vsb) - 4
	if colPath < 120 {
		colPath = 120
	}
	win32.SendMsg(w.hList, win32.LVM_SETCOLUMNWIDTH, 0, uintptr(colName))
	win32.SendMsg(w.hList, win32.LVM_SETCOLUMNWIDTH, 1, uintptr(colTag))
	win32.SendMsg(w.hList, win32.LVM_SETCOLUMNWIDTH, 2, uintptr(colPath))
	win32.SendMsg(w.hList, win32.LVM_SETCOLUMNWIDTH, 3, uintptr(colState))
}

func (w *mainWindow) onDpiChanged(wp, lp uintptr) {
	dpi := win32.Hiword(wp)
	if dpi < 96 {
		return
	}
	ns := float64(dpi) / 96
	if ns == w.scale {
		return
	}
	w.scale = ns
	win32.DeleteObject.Call(w.hFont)
	w.hFont = w.createFont()
	for _, h := range []uintptr{w.hToolbar, w.hList, w.hStatus, w.hFilter} {
		win32.SendMsg(h, win32.WM_SETFONT, w.hFont, 1)
	}
	win32.SendMsg(w.hToolbar, win32.TB_AUTOSIZE, 0, 0)
	w.tbH = win32.ClientHeight(w.hToolbar)

	rc := win32.LparamCopy[win32.Rect](lp)
	win32.SetWindowPos.Call(w.hwnd, 0,
		uintptr(rc.Left), uintptr(rc.Top),
		uintptr(rc.Right-rc.Left), uintptr(rc.Bottom-rc.Top),
		win32.SWP_NOZORDER)
	w.layout()
}

func statusText(valid bool) string {
	if valid {
		return "正常"
	}
	return "失效"
}

func (w *mainWindow) reloadList() {
	win32.SendMsg(w.hList, win32.WM_SETREDRAW, 0, 0)
	win32.SendMsg(w.hList, win32.LVM_DELETEALLITEMS, 0, 0)
	w.visible = w.visible[:0]
	for i, e := range w.st.Entries {
		if w.filter != "" && e.SysTag != w.filter {
			continue
		}
		row := len(w.visible)
		win32.LvSetItemText(w.hList, row, 0, e.Name)
		win32.LvSetItemText(w.hList, row, 1, e.TagColumnText())
		win32.LvSetItemText(w.hList, row, 2, e.Path)
		win32.LvSetItemText(w.hList, row, 3, statusText(e.Valid))
		w.visible = append(w.visible, i)
	}
	win32.SendMsg(w.hList, win32.WM_SETREDRAW, 1, 0)
	w.updateStatus()
	w.updateButtonStates()
}

func (w *mainWindow) updateStatus() {
	text := fmt.Sprintf("共 %d 项，%d 项失效，%d 项待完善",
		len(w.st.Entries), w.st.InvalidCount(), w.st.CountBySysTag("todo"))
	if w.filter != "" {
		text += fmt.Sprintf("，筛选「%s」显示 %d 项", model.SysTagLabel(w.filter), len(w.visible))
	}
	win32.SendMsg(w.hStatus, win32.SB_SETTEXT, 0, uintptr(unsafe.Pointer(win32.MustUTF16(text))))
}

// updateButtonStates 单选一行后 启动/打开目录/PowerShell/打标/移除 才可用。
func (w *mainWindow) updateButtonStates() {
	enable := win32.LvSelected(w.hList) >= 0
	var flag uintptr
	if enable {
		flag = 1
	}
	for _, id := range []int{idcLaunch, idcOpenDir, idcPowerSh, idcMdDoc, idcTag, idcRemove} {
		win32.SendMsg(w.hToolbar, win32.TB_ENABLEBUTTON, uintptr(id), flag)
	}
}

func (w *mainWindow) save() {
	w.cfg.Entries = w.st.Snapshot()
	if err := model.SaveConfig(w.cfg); err != nil {
		win32.MsgBox(w.hwnd, "保存配置失败", err.Error(), win32.MB_ICONERROR)
	}
}

// onCommand code 是 WM_COMMAND 的 HIWORD：0 = 菜单/按钮，win32.CBN_SELCHANGE = 筛选下拉。
func (w *mainWindow) onCommand(id, code int) {
	if id == idFilter && code == win32.CBN_SELCHANGE {
		w.onFilterChanged()
		return
	}
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
	case idcTag:
		w.tagSelected()
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
		win32.DestroyWindow.Call(w.hwnd)
	}
}

// onFilterChanged 筛选下拉选项 → filter key；下标 0 是"全部"。
func (w *mainWindow) onFilterChanged() {
	sel := int(int32(win32.SendMsg(w.hFilter, win32.CB_GETCURSEL, 0, 0)))
	if sel >= 1 && sel <= len(model.SysTagDefs) {
		w.filter = model.SysTagDefs[sel-1].Key
	} else {
		w.filter = ""
	}
	w.reloadList()
}

// entryAtRow 列表行号（含筛选后的映射）→ 条目；越界返回 nil。
func (w *mainWindow) entryAtRow(row int) *model.Entry {
	if row < 0 || row >= len(w.visible) {
		return nil
	}
	return &w.st.Entries[w.visible[row]]
}

// selectedEntry 返回 store 下标与条目；筛选时列表行号经 visible 映射。
func (w *mainWindow) selectedEntry() (int, *model.Entry) {
	sel := win32.LvSelected(w.hList)
	if e := w.entryAtRow(sel); e != nil {
		return w.visible[sel], e
	}
	return -1, nil
}

// ensureEntryValid 启动/打开前再校验一次，列表是旧状态时不至于启动到不存在的文件。
func (w *mainWindow) ensureEntryValid(e *model.Entry) bool {
	if model.FileExists(e.Path) {
		return true
	}
	win32.MsgBox(w.hwnd, "文件不存在", "该文件已失效：\n"+e.Path, win32.MB_ICONWARN)
	w.st.RefreshValid()
	w.reloadList()
	return false
}

func (w *mainWindow) addExeByPicker() {
	path, err := pickExeFile(w.hwnd, w.defaultPickDir())
	if err != nil {
		win32.MsgBox(w.hwnd, "打开文件选择框失败", err.Error(), win32.MB_ICONERROR)
		return
	}
	if path == "" {
		return
	}
	if w.st.Add("", path) {
		w.save()
		w.reloadList()
	} else {
		win32.MsgBox(w.hwnd, "已存在", "该 EXE 已在列表中：\n"+path, win32.MB_ICONINFO)
	}
}

func (w *mainWindow) defaultPickDir() string {
	if w.cfg.LastScanDir != "" {
		return w.cfg.LastScanDir
	}
	if len(w.st.Entries) > 0 {
		return filepath.Dir(w.st.Entries[0].Path)
	}
	return ""
}

func (w *mainWindow) scanAndImport() {
	root, err := pickScanRoot(w.hwnd, w.cfg.LastScanDir)
	if err != nil {
		win32.MsgBox(w.hwnd, "打开目录选择框失败", err.Error(), win32.MB_ICONERROR)
		return
	}
	if root == "" {
		return
	}
	w.cfg.LastScanDir = root

	results, err := scan.ScanDirExe(root)
	if err != nil {
		win32.MsgBox(w.hwnd, "扫描失败", err.Error(), win32.MB_ICONERROR)
		w.save()
		return
	}
	if len(results) == 0 {
		win32.MsgBox(w.hwnd, "扫描完成", "未在该目录找到任何 exe。", win32.MB_ICONINFO)
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
		win32.MsgBox(w.hwnd, "启动失败", err.Error(), win32.MB_ICONERROR)
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
		win32.MsgBox(w.hwnd, "打开目录失败", err.Error(), win32.MB_ICONERROR)
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
		win32.MsgBox(w.hwnd, "打开 PowerShell 失败", err.Error(), win32.MB_ICONERROR)
	}
}

// tagSelected 打标当前选中条目：系统标签单选 + 用户标签自由文本。
func (w *mainWindow) tagSelected() {
	_, e := w.selectedEntry()
	if e == nil {
		return
	}
	sysTag, userTag, ok := runTagDialog(w, e)
	if !ok {
		return
	}
	e.SysTag = sysTag
	e.UserTag = userTag
	w.save()
	w.reloadList()
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
	hdr := win32.LparamCopy[win32.NMHdr](lp)
	if hdr.IdFrom != idList {
		return 0
	}
	switch hdr.Code {
	case win32.LVN_ITEMCHANGED:
		nmlv := win32.LparamCopy[win32.NMListView](lp)
		if nmlv.UChanged&win32.LVIF_STATE != 0 &&
			(nmlv.UNewState&win32.LVIS_SELECTED) != (nmlv.UOldState&win32.LVIS_SELECTED) {
			w.updateButtonStates()
		}
	case win32.NM_DBLCLK:
		w.launchSelected()
	case win32.NM_RCLICK:
		w.showContextMenu()
		return 1
	case win32.NM_CUSTOMDRAW:
		return w.customDraw(lp)
	}
	return 0
}

// customDraw 失效行整行红字（优先级最高）；正常行的标签列按系统标签着色。
// 必须用指针而非拷贝：要写回 clrText 给控件读。
func (w *mainWindow) customDraw(lp uintptr) uintptr {
	nm := (*win32.NMLVCustomDraw)(win32.PtrFromLparam(lp))
	switch nm.Nmcd.DwDrawStage {
	case win32.CDDS_PREPAINT:
		return win32.CDRF_NOTIFYITEMDRAW
	case win32.CDDS_ITEMPREPAINT:
		e := w.entryAtRow(int(nm.Nmcd.DwItemSpec))
		if e == nil {
			return win32.CDRF_DODEFAULT
		}
		if !e.Valid {
			nm.ClrText = win32.RGB(0xC0, 0x30, 0x30)
			return win32.CDRF_DODEFAULT
		}
		// 返回值同 CDRF_NOTIFYSUBITEMDRAW：进入子项绘制阶段，才能只给标签列上色
		return win32.CDRF_NOTIFYITEMDRAW
	}
	if nm.Nmcd.DwDrawStage&win32.CDDS_SUBITEM != 0 && nm.ISubItem == 1 {
		if e := w.entryAtRow(int(nm.Nmcd.DwItemSpec)); e != nil {
			if c, ok := model.SysTagColor(e.SysTag); ok {
				nm.ClrText = c
			}
		}
	}
	return win32.CDRF_DODEFAULT
}

// showContextMenu 右键菜单命令 ID 与工具栏复用，同一套 WM_COMMAND 分支。
func (w *mainWindow) showContextMenu() {
	menu, _, _ := win32.CreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer win32.DestroyMenu.Call(menu)

	enable := win32.LvSelected(w.hList) >= 0
	appendMenu := func(text string, id int, enabled bool) {
		flags := uintptr(win32.MF_STRING)
		if !enabled {
			flags |= win32.MF_GRAYED
		}
		win32.AppendMenuW.Call(menu, flags, uintptr(id), uintptr(unsafe.Pointer(win32.MustUTF16(text))))
	}
	appendMenu("启动", idcLaunch, enable)
	appendMenu("打开目录", idcOpenDir, enable)
	appendMenu("在此目录开 PowerShell", idcPowerSh, enable)
	appendMenu("查看介绍", idcMdDoc, enable)
	appendMenu("设置标签", idcTag, enable)
	win32.AppendMenuW.Call(menu, win32.MF_SEPARATOR, 0, 0)
	appendMenu("移除", idcRemove, enable)

	var pt win32.Point
	win32.GetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	win32.SetForegroundWindow.Call(w.hwnd)
	win32.TrackPopupMenu.Call(menu, win32.TPM_RIGHTBUTTON,
		uintptr(uint32(pt.X)), uintptr(uint32(pt.Y)), 0, w.hwnd, 0)
	win32.PostMessageW.Call(w.hwnd, win32.WM_NULL, 0, 0)
}
