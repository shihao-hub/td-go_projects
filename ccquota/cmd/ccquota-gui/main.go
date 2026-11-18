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

	"ccquota/internal/server"
	"ccquota/internal/service"
	"ccquota/internal/version"
)

const windowClassName = "ccquotaMainWindow"

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
		OnBeforeClose: app.beforeClose,
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: uniqueID,
			OnSecondInstanceLaunch: func(options.SecondInstanceData) {
				app.showWindow()
			},
		},
		Windows: &windows.Options{
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
			Theme:                windowTheme,
			WindowClassName:      windowClassName,
		},
	})
	if err != nil {
		log.Fatalf("应用退出: %v", err)
	}
}
