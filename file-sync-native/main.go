package main

import (
	"context"
	"embed"
	"log"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"file-sync-native/config"
	"file-sync-native/engine"
	"file-sync-native/logging"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	if err := logging.Init(""); err != nil {
		log.Fatalf("初始化日志失败: %v", err)
	}
	cfg, err := config.Load("")
	if err != nil {
		logging.Errorf("main", "加载配置失败: %v", err)
		log.Fatalf("加载配置失败: %v", err)
	}
	cachePath, err := hashCachePath()
	if err != nil {
		log.Fatalf("定位哈希缓存路径失败: %v", err)
	}
	cache := engine.LoadHashCache(cachePath)

	logging.Infof("main", "File Sync 启动: 配置=%s 缓存=%s 任务数=%d", cfg.Path(), cachePath, len(cfg.ListTasks()))

	app := NewApp(cfg, cache)
	go runTray(app)

	err = wails.Run(&options.App{
		Title:      "File Sync",
		Width:      1024,
		Height:     768,
		MinWidth:   760,
		MinHeight:  520,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 243, G: 245, B: 249, A: 1},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		OnBeforeClose: func(ctx context.Context) bool {
			// 关窗 → 隐藏到托盘；真正的退出走托盘菜单
			runtime.WindowHide(ctx)
			return true
		},
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: "file-sync-native-a1f0c2",
			OnSecondInstanceLaunch: func(data options.SecondInstanceData) {
				// 二次启动：把已有窗口拉到前台
				app.showWindow()
			},
		},
		Windows: &windows.Options{
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
			Theme:                windows.Light,
		},
		Bind: []interface{}{
			app,
		},
	})
	if err != nil {
		logging.Errorf("main", "应用退出: %v", err)
		log.Fatalf("应用退出: %v", err)
	}
}
