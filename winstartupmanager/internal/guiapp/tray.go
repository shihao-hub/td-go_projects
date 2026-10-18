package guiapp

import (
	"winstartupmanager/internal/engine"
	"winstartupmanager/internal/env"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func (a *App) buildTray() {
	tray := a.app.SystemTray.New()
	a.tray = tray

	if len(a.icon) > 0 {
		tray.SetIcon(a.icon)
	}

	tray.SetTooltip(env.WindowTitle())

	menu := a.app.NewMenu()
	menu.Add("打开主界面").OnClick(func(ctx *application.Context) {
		a.ensureMainWindow()
	})

	if !engine.IsProcessElevated() {
		menu.Add("以管理员身份重启").OnClick(func(ctx *application.Context) {
			_ = engine.RestartAsAdmin()
		})
	}

	menu.AddSeparator()
	menu.Add("退出").OnClick(func(ctx *application.Context) {
		a.quit()
	})

	tray.SetMenu(menu)

	tray.OnClick(func() {
		a.ensureMainWindow()
	})
	tray.OnDoubleClick(func() {
		a.ensureMainWindow()
	})
}