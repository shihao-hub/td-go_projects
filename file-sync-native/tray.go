package main

import (
	"context"
	_ "embed"

	"file-sync-native/logging"

	"github.com/getlantern/systray"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed build/windows/icon.ico
var trayIcon []byte

// runTray 在独立 goroutine 上跑托盘（Windows 消息循环按线程独立，
// 与 Wails 主线程互不抢占）。必须从 main 里 go runTray(...) 调用。
func runTray(a *App) {
	systray.Run(func() {
		systray.SetIcon(trayIcon)
		systray.SetTooltip("File Sync · 本地目录同步")

		mOpen := systray.AddMenuItem("打开主窗口", "显示 File Sync 主窗口")
		systray.AddSeparator()
		mQuit := systray.AddMenuItem("退出", "退出 File Sync")

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
	}, func() {
		logging.Infof("tray", "托盘已退出")
	})
}

func (a *App) showWindow() {
	if a.ctx == nil {
		return
	}
	runtime.WindowShow(a.ctx)
}

func (a *App) quitApp() {
	if a.ctx == nil {
		return
	}
	runtime.Quit(a.ctx)
}

// shutdown 是 Wails 退出钩子：摘掉托盘图标。
func (a *App) shutdown(ctx context.Context) {
	systray.Quit()
}
