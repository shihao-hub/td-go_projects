package main

import (
	"context"
	"sync/atomic"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// guiApp 保存 Wails 运行上下文，供托盘回调操作窗口。
type guiApp struct {
	ctx context.Context
	// quitting 区分「用户关窗（收进托盘）」与「真的退出」。
	//
	// Wails v2 的 Frontend.Quit 会先调用 OnBeforeClose，返回 true 就取消退出；
	// 若 OnBeforeClose 无条件隐藏窗口并返回 true，托盘「退出」会被同一个条件拦掉。
	quitting atomic.Bool
}

func (a *guiApp) startup(ctx context.Context) {
	a.ctx = ctx
}

// beforeClose 关窗即隐藏到托盘；只有明确退出时才放行。
func (a *guiApp) beforeClose(ctx context.Context) bool {
	if a.quitting.Load() {
		return false
	}
	runtime.WindowHide(ctx)
	return true
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
	a.quitting.Store(true)
	runtime.Quit(a.ctx)
}
