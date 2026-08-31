package main

import (
	"log"
	"os"
	"runtime"
	"runtime/debug"
	"unsafe"
)

func main() {
	initLogging()
	defer func() {
		if r := recover(); r != nil {
			logFatal("PANIC in main: %v\n%s", r, debug.Stack())
		}
	}()
	log.Printf("启动: 日志文件 %s", logPath())
	runtime.LockOSThread()

	if !acquireSingleInstance() {
		msgBox(0, mainTitle, "程序已在运行，请勿重复启动。", mbIconWarn)
		os.Exit(0)
	}

	installDebugVEH()
	setDpiAwareness()

	icc := initCommonControlsExStr{
		dwSize: uint32(unsafe.Sizeof(initCommonControlsExStr{})),
		dwICC:  iccListviewClasses | iccBarClasses,
	}
	if r, _, _ := pInitCommonControlsEx.Call(uintptr(unsafe.Pointer(&icc))); r == 0 {
		alertError(mainTitle, "InitCommonControlsEx 失败，通用控件不可用")
		os.Exit(1)
	}

	cfg := loadConfig()
	st := newStore(cfg.Entries)
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
	if r, _, _ := pSetProcessDpiAwarenessContext.Call(^uintptr(3)); r != 0 {
		return
	}
	pSetProcessDPIAware.Call()
}

func runMessageLoop() {
	var m msg
	for {
		r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func alertError(title, text string) {
	msgBox(0, title, text, mbIconError)
}
