// Package guiapp 是 typeai-gui 的 Wails v3 组装层：窗口与终端服务装配。
// 终端桥接业务全部在 internal/terminal，本包只做装配与转发。
package guiapp

import (
	"embed"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// RunOptions GUI 启动参数（图标经 PE 资源注入，窗口随 exe 图标）。
type RunOptions struct {
	Assets embed.FS // //go:embed all:frontend/dist 的产物
}

// Run 组装并运行 GUI 应用，阻塞至退出。
func Run(opts RunOptions) {
	svc := &TerminalService{}
	app := application.New(application.Options{
		Name:        "typeai-gui",
		Description: "typeai 终端窗口壳（xterm.js + ConPTY）",
		Services: []application.Service{
			application.NewService(svc),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(opts.Assets),
		},
	})
	svc.Bind(app)

	win := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "typeai",
		Width:            980,
		Height:           640,
		MinWidth:         480,
		MinHeight:        320,
		URL:              "/",
		BackgroundColour: application.NewRGB(12, 12, 12),
	})

	// 关窗即终止终端会话：typeai 被杀、ConPTY 释放，无残留进程
	win.RegisterHook(events.Common.WindowClosing, func(*application.WindowEvent) {
		svc.StopSession()
	})

	if err := app.Run(); err != nil {
		panic(err) // GUI 无法启动属致命错误
	}
}
