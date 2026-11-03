package guiapp

import (
	"winstartupmanager/internal/engine"
	"winstartupmanager/internal/env"
	"winstartupmanager/internal/logx"

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

	// 开机自启：dev 通道置灰，避免污染注册表；release 走 Wails Autostart（HKCU Run）。
	if env.AutostartAllowed() {
		autostartItem := menu.AddCheckbox("开机自启", a.autostartEnabled())
		autostartItem.OnClick(func(ctx *application.Context) {
			enabled := a.autostartEnabled()
			if err := a.setAutostart(!enabled); err != nil {
				logx.Errorf("设置开机自启失败: %v", err)
				return
			}
			autostartItem.SetChecked(!enabled)
			logx.Infof("开机自启已切换为 %v", !enabled)
		})
	} else {
		disabledItem := menu.AddCheckbox("开机自启 (开发版已禁用)", false)
		disabledItem.SetEnabled(false)
	}

	menu.AddSeparator()
	menu.Add("退出").OnClick(func(ctx *application.Context) {
		// 异步退出，避免在 UI 消息泵回调里同步执行清理（家族同款）。
		go a.quit()
	})

	tray.SetMenu(menu)

	tray.OnClick(func() {
		a.ensureMainWindow()
	})
	tray.OnDoubleClick(func() {
		a.ensureMainWindow()
	})
}
