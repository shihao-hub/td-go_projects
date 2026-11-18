package main

import (
	_ "embed"

	"github.com/getlantern/systray"
)

//go:embed icon.ico
var trayIcon []byte

// runTray 在独立 goroutine 上运行托盘：Windows 的消息循环按线程独立，
// 与 Wails 主线程互不抢占。必须从 main 里 go runTray(...) 调用。
func runTray(a *guiApp) {
	systray.Run(func() {
		systray.SetIcon(trayIcon)
		systray.SetTooltip("ccquota · 额度")

		mOpen := systray.AddMenuItem("打开窗口", "显示 ccquota 主窗口")
		systray.AddSeparator()
		mQuit := systray.AddMenuItem("退出", "退出 ccquota")

		go func() {
			for {
				select {
				case <-mOpen.ClickedCh:
					a.showWindow()
				case <-mQuit.ClickedCh:
					a.quitApp()
					return
				}
			}
		}()
	}, func() {})
}
