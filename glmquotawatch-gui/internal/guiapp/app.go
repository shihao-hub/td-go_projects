// Package guiapp 是 glmquotawatch-gui 的 Wails v3 组装层：
// 窗口/托盘/单实例/关窗到托盘/通知/开机自启/演示模式运行时/前端绑定。
// 业务规则全部在 internal/service，本包只做装配与转发。
package guiapp

import (
	"embed"
	"sync/atomic"

	"glmquotawatch-gui/internal/env"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"
)

// RunOptions GUI 启动参数（由 main 装配传入，包内不自持二进制资源，
// 图标与前端产物统一在项目根维护一份）。
type RunOptions struct {
	Demo   bool     // --demo：进入演示模式（60 倍速模拟上游）
	Hidden bool     // --hidden：静默启动（开机自启形态，不弹主窗）
	Assets embed.FS // //go:embed all:frontend/dist 的产物
	Icon   []byte   // build/windows/icon.ico 字节（托盘图标，与 exe 图标同源）
}

// 单实例标识（与 build/config.yml 的 productIdentifier 一致）。
const singleInstanceID = "shihao.langproj.glmquotawatch-gui"

// Run 组装并运行 GUI 应用，阻塞至退出。
//
// 组装顺序（设计不变量 7：第二实例在任何 store 写入前退出）：
// 先纯内存构造 Options（零业务 I/O）→ application.New()——第二实例在此
// os.Exit(0) → 主实例才创建窗口/托盘并启动监控运行时 → app.Run() 阻塞。
func Run(opts RunOptions) {
	bindings := &BindingsService{}
	ns := notifications.New()

	var a *App // OnSecondInstanceLaunch 闭包晚绑定
	app := application.New(application.Options{
		Name:        "glmquotawatch-gui",
		Description: "GLM 编码套餐用量监控（托盘常驻 + 阈值 Toast 告警）",
		Services: []application.Service{
			application.NewService(bindings),
			application.NewService(ns),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(opts.Assets),
		},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: env.SingleInstanceID(),
			ExitCode: 0, // 「新进程自行退出」非错误（AC-4）
			OnSecondInstanceLaunch: func(data application.SecondInstanceData) {
				if a == nil {
					return
				}
				for _, arg := range data.Args {
					if arg == "--demo" { // 主实例已运行时，演示请求就地切换
						go func() { _ = a.rt.EnterDemo() }()
					}
				}
				a.ensureMainWindow()
			},
		},
	})

	a = &App{app: app, icon: opts.Icon}
	a.rt = newMonitorRuntime(app, ns)
	bindings.rt = a.rt // 晚绑定：服务方法在 app 启动后才会被前端调用

	a.buildWindow(opts.Hidden)
	a.buildTray()
	a.rt.SetUI(a) // 托盘就绪后接外观桥
	a.rt.Start(opts.Demo)

	if err := app.Run(); err != nil {
		panic(err) // GUI 无法启动属致命错误；日志走 stderr
	}
}

// App GUI 组装实例：窗口、托盘菜单项与运行时的引用中心。
type App struct {
	app         *application.App
	rt          *MonitorRuntime
	icon        []byte
	win         *application.WebviewWindow
	exiting     atomic.Bool // true 时放行窗口关闭（真退出）
	tray        *application.SystemTray
	silentItem  *application.MenuItem
	autostartIt *application.MenuItem
	demoItem    *application.MenuItem
}

// buildWindow 创建主窗并挂关窗到托盘钩子。
func (a *App) buildWindow(hidden bool) {
	a.win = a.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            env.WindowTitle(),
		Width:            920,
		Height:           560,
		MinWidth:         720,
		MinHeight:        460,
		Hidden:           hidden, // --hidden 静默启动（开机自启）
		URL:              "/",
		BackgroundColour: application.NewRGB(246, 248, 247),
	})
	// 关窗到托盘：hook 先于内置销毁监听器执行，Cancel 阻止销毁
	a.win.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		if a.exiting.Load() {
			return // 真退出放行
		}
		event.Cancel()
		a.win.Hide()
	})
}

// ensureMainWindow 显示主窗；若窗口已被销毁（beta 行为偏差兜底）则重建。
func (a *App) ensureMainWindow() {
	if a.win != nil && a.win.IsVisible() {
		a.win.Focus()
		return
	}
	if a.win != nil {
		a.win.Show()
		a.win.Focus()
		return
	}
	// 窗口意外销毁：按原参数重建（兜底路径，正常不触达）
	a.buildWindow(false)
}

// quit 托盘「退出」：放行窗口关闭 → 停监控 → 退出应用。
func (a *App) quit() {
	a.exiting.Store(true)
	a.rt.Stop()
	a.app.Quit()
}
