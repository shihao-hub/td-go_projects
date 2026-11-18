package main

import (
	"context"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// guiApp 保存 Wails 运行上下文，供托盘回调操作窗口。
type guiApp struct {
	ctx context.Context
}

func (a *guiApp) startup(ctx context.Context) {
	a.ctx = ctx
}

// showWindow 把窗口从托盘恢复到前台。
func (a *guiApp) showWindow() {
	if a.ctx == nil {
		return
	}
	runtime.WindowShow(a.ctx)
	runtime.WindowSetAlwaysOnTop(a.ctx, true)
	runtime.WindowSetAlwaysOnTop(a.ctx, false)
}

// quitApp 退出整个应用（含托盘）。
func (a *guiApp) quitApp() {
	if a.ctx == nil {
		return
	}
	runtime.Quit(a.ctx)
}
