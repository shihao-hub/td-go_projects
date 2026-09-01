package ui

import (
	"exe-launcher/internal/mdview"
	"exe-launcher/internal/win32"
	"runtime"
	"testing"
	"unsafe"
)

// TestRichEditTextRender 集成回归：RICHEDIT50W + EM_SETTEXTEX(ST_RTF) 渲染
// mdview.MDToRTF 产物。同时锁定历史教训：EM_STREAMIN 的流回调路径会触发
// fail-fast(0xC0000409) 硬崩，禁止回退到该方案（见 win32.go win32.SetTextEx 注释）。
func TestRichEditTextRender(t *testing.T) {
	runtime.LockOSThread()
	hInst, _, _ := win32.GetModuleHandleW.Call(0)
	if h, _, _ := win32.LoadLibraryW.Call(uintptr(unsafe.Pointer(win32.MustUTF16("msftedit.dll")))); h == 0 {
		t.Fatal("msftedit.dll 加载失败")
	}

	hwnd, _, _ := win32.CreateWindowExW.Call(0,
		uintptr(unsafe.Pointer(win32.MustUTF16("STATIC"))), 0, uintptr(win32.WS_OVERLAPPEDWINDOW),
		0, 0, 100, 100, 0, 0, hInst, 0)
	if hwnd == 0 {
		t.Fatal("父窗口创建失败")
	}
	hEdit, _, _ := win32.CreateWindowExW.Call(win32.WS_EX_CLIENTEDGE,
		uintptr(unsafe.Pointer(win32.MustUTF16("RICHEDIT50W"))),
		0, uintptr(win32.WS_CHILD|win32.WS_VISIBLE|win32.WS_VSCROLL|win32.ES_MULTILINE|win32.ES_AUTOVSCROLL|win32.ES_READONLY),
		0, 0, 200, 200, hwnd, 0, hInst, 0)
	if hEdit == 0 {
		win32.DestroyWindow.Call(hwnd)
		t.Fatal("RichEdit 创建失败")
	}

	rtf := append([]byte(mdview.MDToRTF("# 标题\n\n正文 **加粗** 和 `code`\n\n- 列表项\n")), 0)
	st := win32.SetTextEx{Flags: win32.ST_RTF, CodePage: 1}
	r, _, _ := win32.SendMessageW.Call(hEdit, win32.EM_SETTEXTEX,
		uintptr(unsafe.Pointer(&st)), uintptr(unsafe.Pointer(&rtf[0])))
	if r == 0 {
		win32.DestroyWindow.Call(hEdit)
		win32.DestroyWindow.Call(hwnd)
		t.Fatal("EM_SETTEXTEX 返回 0，RTF 未被接受")
	}

	const wmGetTextLength = 0x000E
	if tl := win32.SendMsg(hEdit, wmGetTextLength, 0, 0); tl == 0 {
		t.Error("渲染后控件文本长度为 0")
	} else {
		t.Logf("渲染成功，文本长度 %d", tl)
	}

	win32.DestroyWindow.Call(hEdit)
	win32.DestroyWindow.Call(hwnd)
}
