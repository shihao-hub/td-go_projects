package main

import (
	"runtime"
	"testing"
	"unsafe"
)

// TestRichEditTextRender 集成回归：RICHEDIT50W + EM_SETTEXTEX(ST_RTF) 渲染
// mdToRTF 产物。同时锁定历史教训：EM_STREAMIN 的流回调路径会触发
// fail-fast(0xC0000409) 硬崩，禁止回退到该方案（见 win32.go setTextEx 注释）。
func TestRichEditTextRender(t *testing.T) {
	runtime.LockOSThread()
	hInst, _, _ := pGetModuleHandleW.Call(0)
	if h, _, _ := pLoadLibraryW.Call(uintptr(unsafe.Pointer(mustUTF16("msftedit.dll")))); h == 0 {
		t.Fatal("msftedit.dll 加载失败")
	}

	hwnd, _, _ := pCreateWindowExW.Call(0,
		uintptr(unsafe.Pointer(mustUTF16("STATIC"))), 0, uintptr(wsOverlappedWindow),
		0, 0, 100, 100, 0, 0, hInst, 0)
	if hwnd == 0 {
		t.Fatal("父窗口创建失败")
	}
	hEdit, _, _ := pCreateWindowExW.Call(wsExClientEdge,
		uintptr(unsafe.Pointer(mustUTF16("RICHEDIT50W"))),
		0, uintptr(wsChild|wsVisible|wsVScroll|esMultiline|esAutoVScroll|esReadOnly),
		0, 0, 200, 200, hwnd, 0, hInst, 0)
	if hEdit == 0 {
		pDestroyWindow.Call(hwnd)
		t.Fatal("RichEdit 创建失败")
	}

	rtf := append([]byte(mdToRTF("# 标题\n\n正文 **加粗** 和 `code`\n\n- 列表项\n")), 0)
	st := setTextEx{flags: stRtf, codePage: 1}
	r, _, _ := pSendMessageW.Call(hEdit, emSettextex,
		uintptr(unsafe.Pointer(&st)), uintptr(unsafe.Pointer(&rtf[0])))
	if r == 0 {
		pDestroyWindow.Call(hEdit)
		pDestroyWindow.Call(hwnd)
		t.Fatal("EM_SETTEXTEX 返回 0，RTF 未被接受")
	}

	const wmGetTextLength = 0x000E
	if tl := sendMsg(hEdit, wmGetTextLength, 0, 0); tl == 0 {
		t.Error("渲染后控件文本长度为 0")
	} else {
		t.Logf("渲染成功，文本长度 %d", tl)
	}

	pDestroyWindow.Call(hEdit)
	pDestroyWindow.Call(hwnd)
}
