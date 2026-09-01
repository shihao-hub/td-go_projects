package ui

import (
	"log"
	"os"
	"runtime"
	"unsafe"

	"exe-launcher/internal/model"
	"exe-launcher/internal/win32"
)

// Run 应用主流程：单实例检查 → 初始化 → 建主窗口与托盘 → 进入消息循环。
func Run() {
	log.Printf("启动: 日志文件 %s", LogPath())
	runtime.LockOSThread()

	if !acquireSingleInstance() {
		win32.MsgBox(0, mainTitle, "程序已在运行，请勿重复启动。", win32.MB_ICONWARN)
		os.Exit(0)
	}

	installDebugVEH()
	setDpiAwareness()

	icc := win32.InitCommonControlsExStr{
		DwSize: uint32(unsafe.Sizeof(win32.InitCommonControlsExStr{})),
		DwICC:  win32.ICC_LISTVIEW_CLASSES | win32.ICC_BAR_CLASSES,
	}
	if r, _, _ := win32.InitCommonControlsEx.Call(uintptr(unsafe.Pointer(&icc))); r == 0 {
		alertError(mainTitle, "InitCommonControlsEx 失败，通用控件不可用")
		os.Exit(1)
	}

	cfg := model.LoadConfig()
	st := model.NewStore(cfg.Entries)
	st.RefreshValid()

	if _, err := createMainWindow(st, cfg); err != nil {
		alertError(mainTitle, "创建主窗口失败: "+err.Error())
		os.Exit(1)
	}
	trayInit(mainWin.hwnd)
	runMessageLoop()
}

// setDpiAwareness PerMonitorV2 优先（manifest 里也声明了，这里兜底），老系统回落 SetProcessDPIAware。
func setDpiAwareness() {
	if r, _, _ := win32.SetProcessDpiAwarenessContext.Call(^uintptr(3)); r != 0 {
		return
	}
	win32.SetProcessDPIAware.Call()
}

func runMessageLoop() {
	var m win32.Msg
	for {
		r, _, _ := win32.GetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		win32.TranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		win32.DispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func alertError(title, text string) {
	win32.MsgBox(0, title, text, win32.MB_ICONERROR)
}
