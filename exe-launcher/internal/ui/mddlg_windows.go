package ui

import (
	"exe-launcher/internal/mdview"
	"exe-launcher/internal/model"
	"exe-launcher/internal/win32"
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
	hInst, _, _ := win32.GetModuleHandleW.Call(0)
	// RichEdit 4.1 必须先显式加载 msftedit.dll，RICHEDIT50W 类才可用
	if h, _, _ := win32.LoadLibraryW.Call(uintptr(unsafe.Pointer(win32.MustUTF16("msftedit.dll")))); h == 0 {
		win32.MsgBox(owner.hwnd, "初始化失败", "加载 msftedit.dll 失败，无法渲染介绍。", win32.MB_ICONERROR)
		return
	}
	if mdWndProcCb == 0 {
		mdWndProcCb = syscall.NewCallback(mdWndProc)
		wc := win32.WndClassExW{
			CbSize:        uint32(unsafe.Sizeof(win32.WndClassExW{})),
			LpfnWndProc:   mdWndProcCb,
			HInstance:     hInst,
			HCursor:       loadArrowCursor(),
			HbrBackground: win32.COLOR_WINDOW + 1,
			LpszClassName: win32.MustUTF16(mdClassName),
		}
		if atom, _, _ := win32.RegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); atom == 0 {
			return
		}
	}

	d := &mdDialog{scale: owner.scale}
	mdDlg = d

	width := int(720 * d.scale)
	height := int(560 * d.scale)
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

	hwnd, _, err := win32.CreateWindowExW.Call(0,
		uintptr(unsafe.Pointer(win32.MustUTF16(mdClassName))),
		uintptr(unsafe.Pointer(win32.MustUTF16(name+" - 介绍"))),
		win32.WS_OVERLAPPEDWINDOW,
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
	style := uintptr(win32.WS_CHILD | win32.WS_VISIBLE | win32.WS_VSCROLL | win32.WS_TABSTOP | win32.ES_MULTILINE | win32.ES_AUTOVSCROLL | win32.ES_READONLY)
	d.hEdit, _, err = win32.CreateWindowExW.Call(win32.WS_EX_CLIENTEDGE,
		uintptr(unsafe.Pointer(win32.MustUTF16("RICHEDIT50W"))),
		0, style, 0, 0, 0, 0, hwnd, 0, hInst, 0)
	if d.hEdit == 0 {
		log.Printf("创建 RichEdit 失败: %v", err)
		win32.DestroyWindow.Call(hwnd)
		mdDlg = nil
		return
	}

	// EM_SETTEXTEX(ST_RTF) 直接灌 RTF 字符串（NUL 结尾），无回调路径
	rtf := append([]byte(mdview.MDToRTF(markdown)), 0)
	st := win32.SetTextEx{Flags: win32.ST_RTF, CodePage: 1 /*CP_ACP，RTF 本体是纯 ASCII*/}
	r, _, _ := win32.SendMessageW.Call(d.hEdit, win32.EM_SETTEXTEX,
		uintptr(unsafe.Pointer(&st)), uintptr(unsafe.Pointer(&rtf[0])))
	if r == 0 {
		log.Printf("RTF 灌入失败 (EM_SETTEXTEX)")
	}
	win32.SendMsg(d.hEdit, win32.EM_SETSEL, 0, 0)

	d.layout()
	win32.EnableWindow.Call(owner.hwnd, 0)
	win32.ShowWindow.Call(hwnd, win32.SW_SHOW)
	win32.UpdateWindow.Call(hwnd)
	win32.SetFocus.Call(d.hEdit)

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
	mdDlg = nil
	if quit {
		win32.PostQuitMessage.Call(m.WParam)
	}
}

func mdWndProc(hwnd uintptr, msg uint32, wp, lp uintptr) uintptr {
	defer recoverLog("介绍窗口 wndproc")
	d := mdDlg
	if d == nil || d.hwnd != hwnd {
		r, _, _ := win32.DefWindowProcW.Call(hwnd, uintptr(msg), wp, lp)
		return r
	}
	switch msg {
	case win32.WM_SIZE:
		d.layout()
		return 0
	case win32.WM_CLOSE:
		win32.DestroyWindow.Call(hwnd)
		return 0
	}
	r, _, _ := win32.DefWindowProcW.Call(hwnd, uintptr(msg), wp, lp)
	return r
}

func (d *mdDialog) layout() {
	var rc win32.Rect
	win32.GetClientRect.Call(d.hwnd, uintptr(unsafe.Pointer(&rc)))
	cw := int(rc.Right - rc.Left)
	ch := int(rc.Bottom - rc.Top)
	if cw <= 0 || ch <= 0 {
		return
	}
	m := int(8 * d.scale)
	win32.MoveWindow.Call(d.hEdit, uintptr(m), uintptr(m), uintptr(cw-2*m), uintptr(ch-2*m), 1)
}

// showMdDoc 工具栏/右键「介绍」命令：在 exe 同目录找同名 .md 并渲染。
// exe 本身失效不拦（md 是否存在才是关键）。
func (w *mainWindow) showMdDoc() {
	_, e := w.selectedEntry()
	if e == nil {
		return
	}
	mdPath := filepath.Join(filepath.Dir(e.Path), model.ExeBaseName(e.Path)+".md")
	data, err := os.ReadFile(mdPath)
	if err != nil {
		win32.MsgBox(w.hwnd, "未找到介绍文件",
			fmt.Sprintf("未找到同名介绍文件：\n%s", mdPath), win32.MB_ICONINFO)
		return
	}
	log.Printf("查看介绍: %q", mdPath)
	runMdDialog(w, e.Name, string(data))
}
