package app

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"plansql/internal/daemon"
	"plansql/internal/instance"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/w32"
)

const windowTitle = "plansql - Plan/Spec 状态工作台"

type App struct {
	app       *application.App
	server    *daemon.Server
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

// LaunchGUI 启动内嵌 HTTP 服务 + Wails 桌面壳。
// 监听端口固定传 0（动态探测），dev 与 release 实例可并行运行而互不冲突。
func LaunchGUI(hidden bool) error {
	root, remembered := ResolveInitialProjectRoot()

	server, err := daemon.NewServer(daemon.Config{
		Addr:       "127.0.0.1:0",
		RootDir:    root,
		SQLFile:    filepath.Join(root, "plans_status.sql"),
		NoAddrFile: true,
		OnRootChange: func(newRoot, _ string) {
			saveLastProjectRoot(newRoot)
		},
	})
	if err != nil {
		return fmt.Errorf("初始化项目服务失败: %w", err)
	}

	listener, err := daemon.Listen("127.0.0.1:0")
	if err != nil {
		_ = server.Stop()
		return fmt.Errorf("启动 HTTP 监听失败: %w", err)
	}
	serverURL := daemon.URL(listener)
	logf("WebUI 已启动: %s", serverURL)
	logf("项目根: %s", root)
	logf("版本: %s (build: %s, mode: %s, channel: %s)", Version(), BuildID(), BuildMode(), Channel())

	go func() {
		_ = server.Serve(listener)
	}()

	var appInstance *App

	wailsApp := application.New(application.Options{
		Name:        appName,
		Description: "plansql - Plan/Spec 状态管理桌面工作台",
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
			UniqueID: instance.GUIID(IsRelease()),
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
		server:    server,
		serverURL: serverURL,
		winState:  loadWindowState(),
	}

	appInstance.buildTray()
	if !hidden {
		appInstance.ensureMainWindow()
		if !remembered {
			// 首次启动没有历史项目：窗口出现后弹出目录选择，避免双击落到 exe 所在目录。
			go appInstance.promptProjectRoot(true)
		}
	}

	return wailsApp.Run()
}

// currentRoot 返回当前观察的项目根。
func (a *App) currentRoot() string {
	root, _ := a.server.Project()
	return root
}

// switchProject 切换项目根并刷新窗口；失败的切换不会破坏当前状态。
func (a *App) switchProject(root string) error {
	if err := a.server.Reconfigure(root, ""); err != nil {
		return err
	}
	saveLastProjectRoot(root)
	logf("已切换项目根: %s", root)
	if win := a.aliveWindow(); win != nil {
		win.Reload()
	}
	return nil
}

// promptProjectRoot 弹出原生目录选择器；firstRun 为真时先等消息循环与窗口就绪。
func (a *App) promptProjectRoot(firstRun bool) {
	if firstRun {
		time.Sleep(400 * time.Millisecond)
	}

	dialog := a.app.Dialog.OpenFileWithOptions(&application.OpenFileDialogOptions{
		CanChooseDirectories: true,
		CanChooseFiles:       false,
		Title:                "plansql - 选择项目根目录",
		Message:              "选择包含 plans_status.sql 或 .git 的项目根目录",
		ButtonText:           "打开此项目",
		Directory:            a.currentRoot(),
	})
	if win := a.aliveWindow(); win != nil {
		dialog.AttachToWindow(win)
	}

	root, err := dialog.PromptForSingleSelection()
	if err != nil {
		logf("选择项目目录失败: %v", err)
		return
	}
	if root == "" {
		return
	}
	if err := a.switchProject(root); err != nil {
		logf("切换项目目录失败: %v", err)
		a.app.Dialog.Error().
			SetTitle("无法打开项目").
			SetMessage(fmt.Sprintf("%s\n\n%v", root, err)).
			Show()
	}
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
		BackgroundColour: application.NewRGB(22, 22, 28),
	}
	if state.HasPos {
		opts.InitialPosition = application.WindowXY
		opts.X = state.X
		opts.Y = state.Y
	}

	win := a.app.Window.NewWithOptions(opts)
	a.win = win

	// 注册窗口显示与激活钩子，HWND 创建就绪后即刻写入大小图标与窗口类属性
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
	a.tray.SetTooltip("plansql - Plan/Spec 状态工作台 " + Version())

	a.tray.OnClick(a.ensureMainWindow)
	a.tray.OnDoubleClick(a.ensureMainWindow)

	menu := a.app.NewMenu()
	menu.Add("打开工作台").OnClick(func(*application.Context) {
		a.ensureMainWindow()
	})
	menu.Add("在默认浏览器中打开").OnClick(func(*application.Context) {
		_ = openURL(a.serverURL)
	})
	menu.Add("切换项目目录…").OnClick(func(*application.Context) {
		go a.promptProjectRoot(false)
	})
	menu.AddSeparator()

	// 开机自启动项：Dev 模式置灰禁用，Release 模式正常可选
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

	// 关闭内嵌 HTTP 服务与投影库连接后再退出进程。
	if a.server != nil {
		_ = a.server.Stop()
	}

	a.app.Quit()
}

func openURL(url string) error {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", url}
	case "darwin":
		cmd = "open"
		args = []string{url}
	default:
		cmd = "xdg-open"
		args = []string{url}
	}
	return exec.Command(cmd, args...).Start()
}

// OpenBrowser 用系统默认浏览器打开 URL，供 GUI 降级服务复用。
func OpenBrowser(url string) error {
	return openURL(url)
}
