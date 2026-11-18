// ccquota-gui 是 Wails v2 桌面壳：托盘常驻 + 窗口展示同一份 WebUI。
//
// 前端不走 Wails 绑定，而是直接复用 internal/server 的 HTTP handler，
// 因此 WebUI 与 GUI 共用同一套页面和接口，不存在两份实现。
package main

import (
	"context"
	"log"
	"os"

	"github.com/getlantern/systray"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"ccquota/internal/server"
	"ccquota/internal/service"
	"ccquota/internal/version"
)

func main() {
	svc, err := service.New(os.Getenv("CCQUOTA_DB"))
	if err != nil {
		log.Fatalf("初始化失败: %v", err)
	}

	app := &guiApp{}
	go runTray(app)

	uniqueID := "ccquota-gui-4c9f21"
	if !version.IsRelease() {
		uniqueID += ".dev"
	}

	err = wails.Run(&options.App{
		Title:     "ccquota · 额度",
		Width:     1000,
		Height:    760,
		MinWidth:  720,
		MinHeight: 480,
		// 前端资源直接来自 internal/server 的 embed；GUI 与 WebUI 同源。
		AssetServer:      &assetserver.Options{Handler: server.Handler(svc)},
		BackgroundColour: &options.RGBA{R: 23, G: 24, B: 28, A: 1},
		OnStartup:        app.startup,
		OnShutdown: func(ctx context.Context) {
			systray.Quit()
		},
		OnBeforeClose: func(ctx context.Context) bool {
			// 关窗 = 收进托盘，只有托盘「退出」才真正结束进程。
			runtime.WindowHide(ctx)
			return true
		},
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: uniqueID,
			OnSecondInstanceLaunch: func(options.SecondInstanceData) {
				app.showWindow()
			},
		},
		Windows: &windows.Options{
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
			Theme:                windows.Dark,
		},
	})
	if err != nil {
		log.Fatalf("应用退出: %v", err)
	}
}
