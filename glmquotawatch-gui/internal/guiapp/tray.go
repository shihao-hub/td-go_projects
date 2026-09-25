// tray.go：系统托盘与菜单（App 的 uiBridge 实现一并在此）。
package guiapp

import (
	"github.com/wailsapp/wails/v3/pkg/application"
)

// buildTray 创建托盘图标与菜单。
func (a *App) buildTray() {
	a.tray = a.app.SystemTray.New()
	a.tray.SetIcon(a.icon)
	a.tray.SetTooltip("GLM 用量监控")

	menu := a.app.NewMenu()
	menu.Add("显示主窗").OnClick(func(*application.Context) { a.ensureMainWindow() })
	menu.Add("立即采样").OnClick(func(*application.Context) {
		if _, err := a.rt.SampleNow(); err != nil {
			a.rt.ui.NotifyError("立即采样失败", err.Error())
		}
	})
	a.silentItem = menu.AddCheckbox("静音通知", false)
	a.silentItem.OnClick(func(*application.Context) {
		if err := a.rt.ToggleSilent(); err != nil {
			a.rt.ui.NotifyError("切换静音失败", err.Error())
		}
	})
	a.autostartIt = menu.AddCheckbox("开机自启", false)
	a.autostartIt.OnClick(func(*application.Context) {
		if err := a.rt.ToggleAutostart(); err != nil {
			a.rt.ui.NotifyError("设置开机自启失败", err.Error())
		}
	})
	a.demoItem = menu.Add("进入演示模式")
	a.demoItem.OnClick(func(*application.Context) {
		if a.rt.Mode() == "demo" {
			if err := a.rt.ExitDemo(); err != nil {
				a.rt.ui.NotifyError("退出演示失败", err.Error())
			}
			return
		}
		if err := a.rt.EnterDemo(); err != nil {
			a.rt.ui.NotifyError("进入演示失败", err.Error())
		}
	})
	menu.AddSeparator()
	menu.Add("退出").OnClick(func(*application.Context) { a.quit() })
	a.tray.SetMenu(menu)

	// 初始勾选状态（静音随配置刷新发生在 rt.Start 后首轮；自启在此直接查）
	a.autostartIt.SetChecked(a.rt.AutostartEnabled())
}

// ---- uiBridge 实现：runtime → 托盘/菜单 的单向更新 ----

// SetTooltip 更新托盘 tooltip（三态文案由 runtime 计算）。
func (a *App) SetTooltip(text string) {
	if a.tray != nil {
		a.tray.SetTooltip(text)
	}
}

// SetSilentChecked 同步托盘静音勾选。
func (a *App) SetSilentChecked(checked bool) {
	if a.silentItem != nil {
		a.silentItem.SetChecked(checked)
	}
}

// SetAutostartChecked 同步托盘自启勾选。
func (a *App) SetAutostartChecked(checked bool) {
	if a.autostartIt != nil {
		a.autostartIt.SetChecked(checked)
	}
}

// SetDemoMode 切换演示模式下的菜单形态：演示项文案互斥 + 静音/自启置灰。
func (a *App) SetDemoMode(demo bool) {
	if a.demoItem != nil {
		if demo {
			a.demoItem.SetLabel("退出演示模式")
		} else {
			a.demoItem.SetLabel("进入演示模式")
		}
	}
	if a.silentItem != nil {
		a.silentItem.SetEnabled(!demo)
	}
}

// NotifyError 以系统对话框提示错误（托盘操作失败可见可感）。
func (a *App) NotifyError(title, message string) {
	a.app.Dialog.Error().SetTitle(title).SetMessage(message).Show()
}
