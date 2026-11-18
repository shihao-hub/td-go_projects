package main

import (
	"fyne.io/systray"
)

// runTray 在独立 goroutine 上运行托盘：Windows 的消息循环按线程独立，
// 与 Wails 主线程互不抢占。必须从 main 里 go runTray(...) 调用。
//
// 交互遵循 Windows 惯例：左键单击显示窗口，右键弹菜单。
// getlantern/systray 把左右键都接到 showMenu，做不到这一点，故用 fyne 的 fork。
func runTray(a *guiApp) {
	systray.Run(func() {
		systray.SetIcon(trayIcon)
		systray.SetTooltip("ccquota · 额度")

		// 左键：直接开窗。
		systray.SetOnTapped(func() { a.showWindow() })
		// 右键不注册回调，fyne 的默认行为就是弹出菜单。

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
