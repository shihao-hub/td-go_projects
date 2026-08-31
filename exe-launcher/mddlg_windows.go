package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

// Markdown 介绍对话框：RICHEDIT50W (RichEdit 4.1) 渲染 md→RTF。
// 模态模式与扫描对话框一致：禁用主窗 + 本地消息循环 + WM_QUIT 透传。

const mdClassName = "ExeLauncherMdWnd"

type mdDialog struct {
	hwnd  uintptr
	hEdit uintptr
	scale float64
}

var (
	mdDlg       *mdDialog
	mdWndProcCb uintptr
)

// runMdDialog 弹出模态窗口渲染 markdown 文本。
func runMdDialog(owner *mainWindow, name, markdown string) {
	hInst, _, _ := pGetModuleHandleW.Call(0)
	// RichEdit 4.1 必须先显式加载 msftedit.dll，RICHEDIT50W 类才可用
	if h, _, _ := pLoadLibraryW.Call(uintptr(unsafe.Pointer(mustUTF16("msftedit.dll")))); h == 0 {
		msgBox(owner.hwnd, "初始化失败", "加载 msftedit.dll 失败，无法渲染介绍。", mbIconError)
		return
	}
	if mdWndProcCb == 0 {
		mdWndProcCb = syscall.NewCallback(mdWndProc)
		wc := wndClassExW{
			cbSize:        uint32(unsafe.Sizeof(wndClassExW{})),
			lpfnWndProc:   mdWndProcCb,
			hInstance:     hInst,
			hCursor:       loadArrowCursor(),
			hbrBackground: colorWindow + 1,
			lpszClassName: mustUTF16(mdClassName),
		}
		if atom, _, _ := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); atom == 0 {
			return
		}
	}

	d := &mdDialog{scale: owner.scale}
	mdDlg = d

	width := int(720 * d.scale)
	height := int(560 * d.scale)
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

	hwnd, _, err := pCreateWindowExW.Call(0,
		uintptr(unsafe.Pointer(mustUTF16(mdClassName))),
		uintptr(unsafe.Pointer(mustUTF16(name+" - 介绍"))),
		wsOverlappedWindow,
		uintptr(uint32(x)), uintptr(uint32(y)),
		uintptr(uint32(width)), uintptr(uint32(height)),
		owner.hwnd, 0, hInst, 0)
	if hwnd == 0 {
		mdDlg = nil
		log.Printf("创建介绍窗口失败: %v", err)
		return
	}
	d.hwnd = hwnd

	// 只读 + 垂直滚动，宽度自适应换行由 RichEdit 默认 word-wrap 负责
	style := uintptr(wsChild | wsVisible | wsVScroll | wsTabStop | esMultiline | esAutoVScroll | esReadOnly)
	d.hEdit, _, err = pCreateWindowExW.Call(wsExClientEdge,
		uintptr(unsafe.Pointer(mustUTF16("RICHEDIT50W"))),
		0, style, 0, 0, 0, 0, hwnd, 0, hInst, 0)
	if d.hEdit == 0 {
		log.Printf("创建 RichEdit 失败: %v", err)
		pDestroyWindow.Call(hwnd)
		mdDlg = nil
		return
	}

	// EM_SETTEXTEX(ST_RTF) 直接灌 RTF 字符串（NUL 结尾），无回调路径
	rtf := append([]byte(mdToRTF(markdown)), 0)
	st := setTextEx{flags: stRtf, codePage: 1 /*CP_ACP，RTF 本体是纯 ASCII*/}
	r, _, _ := pSendMessageW.Call(d.hEdit, emSettextex,
		uintptr(unsafe.Pointer(&st)), uintptr(unsafe.Pointer(&rtf[0])))
	if r == 0 {
		log.Printf("RTF 灌入失败 (EM_SETTEXTEX)")
	}
	sendMsg(d.hEdit, emSetSel, 0, 0)

	d.layout()
	pEnableWindow.Call(owner.hwnd, 0)
	pShowWindow.Call(hwnd, swShow)
	pUpdateWindow.Call(hwnd)
	pSetFocus.Call(d.hEdit)

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
	mdDlg = nil
	if quit {
		pPostQuitMessage.Call(m.wParam)
	}
}

func mdWndProc(hwnd uintptr, msg uint32, wp, lp uintptr) uintptr {
	defer recoverLog("介绍窗口 wndproc")
	d := mdDlg
	if d == nil || d.hwnd != hwnd {
		r, _, _ := pDefWindowProcW.Call(hwnd, uintptr(msg), wp, lp)
		return r
	}
	switch msg {
	case wmSize:
		d.layout()
		return 0
	case wmClose:
		pDestroyWindow.Call(hwnd)
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, uintptr(msg), wp, lp)
	return r
}

func (d *mdDialog) layout() {
	var rc rect
	pGetClientRect.Call(d.hwnd, uintptr(unsafe.Pointer(&rc)))
	cw := int(rc.right - rc.left)
	ch := int(rc.bottom - rc.top)
	if cw <= 0 || ch <= 0 {
		return
	}
	m := int(8 * d.scale)
	pMoveWindow.Call(d.hEdit, uintptr(m), uintptr(m), uintptr(cw-2*m), uintptr(ch-2*m), 1)
}

// showMdDoc 工具栏/右键「介绍」命令：在 exe 同目录找同名 .md 并渲染。
// exe 本身失效不拦（md 是否存在才是关键）。
func (w *mainWindow) showMdDoc() {
	_, e := w.selectedEntry()
	if e == nil {
		return
	}
	mdPath := filepath.Join(filepath.Dir(e.Path), exeBaseName(e.Path)+".md")
	data, err := os.ReadFile(mdPath)
	if err != nil {
		msgBox(w.hwnd, "未找到介绍文件",
			fmt.Sprintf("未找到同名介绍文件：\n%s", mdPath), mbIconInfo)
		return
	}
	log.Printf("查看介绍: %q", mdPath)
	runMdDialog(w, e.Name, string(data))
}
