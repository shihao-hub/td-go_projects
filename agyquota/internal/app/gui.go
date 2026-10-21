package app

import (
	"os/exec"
	"runtime"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/w32"
)

const (
	appName     = "agyquota"
	windowTitle = "Antigravity 配额 - agyquota"
)

// App 是桌面壳实例：持有 Wails 应用、内嵌 WebUI 地址与窗口几何状态。
type App struct {
	app       *application.App
	serverURL string

	winMu sync.Mutex
	win   *application.WebviewWindow

	// 窗口几何快照：启动时从 window.json 读取，之后随用户操作回写。
	stateMu    sync.Mutex
	stateTimer *time.Timer
	winState   windowState

	ensureMu sync.Mutex
	tray     *application.SystemTray
	exiting  atomic.Bool
}

// LaunchGUI 启动内嵌 WebUI 与 Wails 桌面壳。port 传 0 时由系统动态探测可用端口
// （默认行为，dev 与 release 实例可并行运行而互不冲突）。
func LaunchGUI(port int, hidden bool) error {
	serverURL, err := startEmbeddedServer(port)
	if err != nil {
		return err
	}

	var appInstance *App

	// 单实例互斥 ID 按构建通道隔离，避免 dev 调试实例与常驻正式版互相挤掉。
	singleInstanceID := "shihao.langproj.agyquota.gui"
	if !IsRelease() {
		singleInstanceID += ".dev"
	}

	wailsApp := application.New(application.Options{
		Name:        appName,
		Description: "Antigravity 模型配额桌面客户端",
		Icon:        iconICO,
		Windows: application.WindowsOptions{
			DisableQuitOnLastWindowClosed: true,
			WebviewUserDataPath:           webviewDataPath(),
			AdditionalBrowserArgs: []string{
				"--disable-gpu",
				"--disable-gpu-compositing",
				"--disable-software-rasterizer",
				"--renderer-process-limit=1",
				"--disable-features=NetworkServiceOutOfProcess",
			},
		},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: singleInstanceID,
			ExitCode: 0,
			OnSecondInstanceLaunch: func(data application.SecondInstanceData) {
				if appInstance != nil {
					appInstance.ensureMainWindow()
				}
			},
		},
	})

	appInstance = &App{
		app:       wailsApp,
		serverURL: serverURL,
		winState:  loadWindowState(),
	}

	appInstance.buildTray()
	if !hidden {
		appInstance.ensureMainWindow()
	}

	return wailsApp.Run()
}

func (a *App) buildWindowLocked(hidden bool) {
	state := a.currentWindowState()
	opts := application.WebviewWindowOptions{
		Title:            windowTitle,
		Width:            state.Width,
		Height:           state.Height,
		MinWidth:         minWindowWidth,
		MinHeight:        minWindowHeight,
		Hidden:           hidden,
		URL:              a.serverURL,
		BackgroundColour: application.NewRGB(244, 242, 236),
	}
	if state.HasPos {
		opts.InitialPosition = application.WindowXY
		opts.X = state.X
		opts.Y = state.Y
	}

	win := a.app.Window.NewWithOptions(opts)
	a.win = win

	// 注册窗口显示与激活钩子，HWND 创建就绪后即刻写入大小图标与窗口类属性。
	win.RegisterHook(events.Windows.WindowShow, func(*application.WindowEvent) {
		a.applyWindowIcon(win)
		a.ensureWindowOnScreen(win)
	})
	win.RegisterHook(events.Windows.WindowActive, func(*application.WindowEvent) {
		a.applyWindowIcon(win)
	})

	// 几何变更（拖拽 / 缩放 / 最大化 / 还原）防抖落盘，崩溃或强杀也不丢状态。
	for _, evt := range []events.WindowEventType{
		events.Common.WindowDidResize,
		events.Common.WindowDidMove,
		events.Common.WindowMaximise,
		events.Common.WindowUnMaximise,
	} {
		win.OnWindowEvent(evt, func(*application.WindowEvent) {
			a.scheduleSaveWindowState(win)
		})
	}

	win.RegisterHook(events.Common.WindowClosing, func(*application.WindowEvent) {
		// 窗口即将销毁，尺寸信息此后不可读，必须先同步落盘一次。
		a.stopWindowStateTimer()
		a.saveWindowState(win)

		if a.exiting.Load() {
			return
		}

		a.winMu.Lock()
		if a.win == win {
			a.win = nil
		}
		a.winMu.Unlock()

		go func() {
			time.Sleep(250 * time.Millisecond)
			runtime.GC()
			debug.FreeOSMemory()
		}()
	})
}

// currentWindowState 返回启动时生效的几何快照（副本）。
func (a *App) currentWindowState() windowState {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	return a.winState
}

// scheduleSaveWindowState 防抖保存：拖拽 / 缩放过程中事件密集，只在停止操作后落盘一次。
func (a *App) scheduleSaveWindowState(win *application.WebviewWindow) {
	a.stateMu.Lock()
	if a.stateTimer != nil {
		a.stateTimer.Stop()
	}
	a.stateTimer = time.AfterFunc(windowStateSaveDelay*time.Millisecond, func() {
		a.saveWindowState(win)
	})
	a.stateMu.Unlock()
}

func (a *App) stopWindowStateTimer() {
	a.stateMu.Lock()
	if a.stateTimer != nil {
		a.stateTimer.Stop()
		a.stateTimer = nil
	}
	a.stateMu.Unlock()
}

// saveWindowState 采集当前窗口几何并落盘。
//
// 最大化 / 最小化 / 全屏时窗口报告的尺寸不是用户期望的"还原尺寸"，
// 因此只翻转 Maximised 标记，保留上一次记录的还原几何。
func (a *App) saveWindowState(win *application.WebviewWindow) {
	if win == nil {
		return
	}

	state := a.currentWindowState()
	switch {
	case win.IsMinimised(), win.IsFullscreen():
		return
	case win.IsMaximised():
		state.Maximised = true
	default:
		width, height := win.Size()
		if width < minWindowWidth || height < minWindowHeight {
			return
		}
		x, y := win.Position()
		state.Width, state.Height = width, height
		state.X, state.Y, state.HasPos = x, y, true
		state.Maximised = false
	}

	a.stateMu.Lock()
	changed := state != a.winState
	if changed {
		a.winState = state
	}
	a.stateMu.Unlock()
	if !changed {
		return
	}

	if err := writeWindowState(state); err != nil {
		logf("保存窗口状态失败: %v", err)
	}
}

// ensureWindowOnScreen 兜底纠正：恢复的坐标若落在已拔掉的外接屏上，
// 窗口会完全不可见，此时回落到主屏居中并回写新坐标。
func (a *App) ensureWindowOnScreen(win *application.WebviewWindow) {
	if win == nil || win.IsMaximised() || win.IsMinimised() {
		return
	}
	screens := a.app.Screen.GetAll()
	if len(screens) == 0 {
		return
	}

	x, y := win.Position()
	width, height := win.Size()
	centreX, centreY := x+width/2, y+height/2
	for _, screen := range screens {
		area := screen.WorkArea
		if centreX >= area.X && centreX < area.X+area.Width && centreY >= area.Y && centreY < area.Y+area.Height {
			return
		}
	}

	logf("上次窗口位置 (%d,%d) 不在任何屏幕工作区内，回落到主屏居中", x, y)
	win.Center()
	a.scheduleSaveWindowState(win)
}

func (a *App) applyWindowIcon(win *application.WebviewWindow) {
	if win == nil || len(iconICO) == 0 {
		return
	}
	hwnd := w32.HWND(win.NativeWindow())
	if hwnd == 0 {
		return
	}
	if hBig, err := w32.CreateLargeHIconFromImage(iconICO); err == nil && hBig != 0 {
		w32.SendMessage(hwnd, w32.WM_SETICON, w32.ICON_BIG, uintptr(hBig))
		w32.SetApplicationIcon(uintptr(hwnd), hBig)
	}
	if hSmall, err := w32.CreateSmallHIconFromImage(iconICO); err == nil && hSmall != 0 {
		w32.SetWindowIcon(hwnd, hSmall)
	}
}

func (a *App) aliveWindow() *application.WebviewWindow {
	a.winMu.Lock()
	defer a.winMu.Unlock()

	win := a.win
	if win == nil {
		return nil
	}
	if _, exists := a.app.Window.GetByID(win.ID()); !exists {
		a.win = nil
		return nil
	}
	return win
}

func (a *App) ensureMainWindow() {
	go a.showMainWindow()
}

func (a *App) showMainWindow() {
	a.ensureMu.Lock()
	defer a.ensureMu.Unlock()

	if a.exiting.Load() {
		return
	}

	if win := a.aliveWindow(); win != nil {
		if win.IsMinimised() {
			win.UnMinimise()
		}
		win.Show()
		win.Focus()
		a.applyWindowIcon(win)
		return
	}

	a.winMu.Lock()
	a.buildWindowLocked(false)
	win := a.win
	a.winMu.Unlock()
	if win == nil {
		return
	}

	win.Show()
	if a.currentWindowState().Maximised {
		win.Maximise()
	}
	win.Focus()
	a.applyWindowIcon(win)
}

func (a *App) buildTray() {
	a.tray = a.app.SystemTray.New()
	if len(iconICO) > 0 {
		a.tray.SetIcon(iconICO)
	}
	a.tray.SetTooltip("agyquota - Antigravity 配额")

	a.tray.OnClick(a.ensureMainWindow)
	a.tray.OnDoubleClick(a.ensureMainWindow)

	menu := a.app.NewMenu()
	menu.Add("打开配额面板").OnClick(func(*application.Context) {
		a.ensureMainWindow()
	})
	menu.Add("在默认浏览器中打开").OnClick(func(*application.Context) {
		if err := openURL(a.serverURL); err != nil {
			logf("打开浏览器失败: %v", err)
		}
	})
	menu.Add("打开数据目录").OnClick(func(*application.Context) {
		if dir := appDataDir(); dir != "" {
			if err := openPath(dir); err != nil {
				logf("打开数据目录失败: %v", err)
			}
		}
	})
	menu.AddSeparator()

	// 开机自启动项：Dev 模式置灰禁用，Release 模式正常可选。
	if IsRelease() {
		autostartItem := menu.AddCheckbox("开机自启", a.autostartEnabled())
		autostartItem.OnClick(func(*application.Context) {
			enabled := a.autostartEnabled()
			if err := a.setAutostart(!enabled); err != nil {
				logf("设置开机自启失败: %v", err)
			} else {
				autostartItem.SetChecked(!enabled)
			}
		})
	} else {
		disabledItem := menu.AddCheckbox("开机自启 (开发版已禁用)", false)
		disabledItem.SetEnabled(false)
	}

	menu.AddSeparator()
	menu.Add("彻底退出").OnClick(func(*application.Context) {
		go a.quit()
	})

	a.tray.SetMenu(menu)
}

func (a *App) quit() {
	a.exiting.Store(true)

	a.winMu.Lock()
	win := a.win
	a.win = nil
	a.winMu.Unlock()

	if win != nil {
		win.Close()
	}

	a.app.Quit()
}

func openURL(url string) error {
	return openTarget(url)
}

func openPath(path string) error {
	return openTarget(path)
}

func openTarget(target string) error {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", target}
	case "darwin":
		cmd = "open"
		args = []string{target}
	default:
		cmd = "xdg-open"
		args = []string{target}
	}
	return exec.Command(cmd, args...).Start()
}
