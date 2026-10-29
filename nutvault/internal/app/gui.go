package app

import (
	"fmt"
	"os/exec"
	"runtime"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"nutvault/internal/api"
	"nutvault/internal/storage"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/w32"
)

const (
	windowTitle          = "NutVault - 坚果云轻量文件版本备份时光机"
	minWindowWidth       = 800
	minWindowHeight      = 600
	windowStateSaveDelay = 500
)

type App struct {
	app       *application.App
	serverURL string
	store     *storage.Store
	server    *api.Server

	winMu sync.Mutex
	win   *application.WebviewWindow

	stateMu    sync.Mutex
	stateTimer *time.Timer
	winState   windowState

	ensureMu sync.Mutex
	tray     *application.SystemTray
	exiting  atomic.Bool
}

func LaunchGUI(dbPath string, port int, hidden bool) error {
	if dbPath == "" {
		dbPath = DefaultDBPath()
	}
	logf("启动 version=%s mode=%s", Version, BuildMode)
	store, err := storage.NewStore(dbPath)
	if err != nil {
		return fmt.Errorf("打开数据库失败: %w", err)
	}

	listener, err := api.Listen(port)
	if err != nil {
		return fmt.Errorf("启动 HTTP 监听失败: %w", err)
	}
	serverURL := api.URL(listener)
	logf("WebUI 已启动: %s", serverURL)

	var appInstance *App
	singleInstanceID := guiSingleInstanceID(IsRelease())

	wailsApp := application.New(application.Options{
		Logger:      wailsLogger(),
		Name:        appName,
		Description: "NutVault - 坚果云轻量文件版本备份时光机",
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

	server := api.NewServer(store, api.Meta{
		Channel:      Channel(),
		BuildMode:    BuildMode,
		Version:      Version,
		DBPath:       dbPath,
		IsAutoStart:  IsAutoStartEnabled,
		SetAutoStart: SetAutoStart,
		SelectFile: func(title string) (string, error) {
			dialog := wailsApp.Dialog.OpenFile()
			if title != "" {
				dialog.SetTitle(title)
			}
			dialog.CanChooseFiles(true)
			dialog.CanChooseDirectories(false)
			return dialog.PromptForSingleSelection()
		},
		SelectDirectory: func(title string) (string, error) {
			dialog := wailsApp.Dialog.OpenFile()
			if title != "" {
				dialog.SetTitle(title)
			}
			dialog.CanChooseFiles(false)
			dialog.CanChooseDirectories(true)
			return dialog.PromptForSingleSelection()
		},
	})
	go func() {
		_ = server.Serve(listener)
	}()

	appInstance = &App{
		app:       wailsApp,
		serverURL: serverURL,
		store:     store,
		server:    server,
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
		BackgroundColour: application.NewRGB(15, 17, 23),
	}
	if state.X >= 0 && state.Y >= 0 {
		opts.InitialPosition = application.WindowXY
		opts.X = state.X
		opts.Y = state.Y
	}

	win := a.app.Window.NewWithOptions(opts)
	a.win = win

	win.RegisterHook(events.Windows.WindowShow, func(*application.WindowEvent) {
		a.applyWindowIcon(win)
		a.ensureWindowOnScreen(win)
	})
	win.RegisterHook(events.Windows.WindowActive, func(*application.WindowEvent) {
		a.applyWindowIcon(win)
	})

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

func (a *App) currentWindowState() windowState {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	return a.winState
}

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

func (a *App) saveWindowState(win *application.WebviewWindow) {
	if win == nil {
		return
	}

	state := a.currentWindowState()
	switch {
	case win.IsMinimised(), win.IsFullscreen():
		return
	case win.IsMaximised():
		state.IsMaximized = true
	default:
		width, height := win.Size()
		if width < minWindowWidth || height < minWindowHeight {
			return
		}
		x, y := win.Position()
		state.Width = width
		state.Height = height
		state.X = x
		state.Y = y
		state.IsMaximized = false
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

	saveWindowState(state)
}

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

	logf("上次窗口位置 (%d,%d) 不在屏幕工作区内，重置主屏居中", x, y)
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
	if a.currentWindowState().IsMaximized {
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
	a.tray.SetTooltip(windowTitle)

	a.tray.OnClick(a.ensureMainWindow)
	a.tray.OnDoubleClick(a.ensureMainWindow)

	menu := a.app.NewMenu()
	menu.Add("打开工作台").OnClick(func(*application.Context) {
		a.ensureMainWindow()
	})
	menu.Add("立即备份全部").OnClick(func(*application.Context) {
		if a.server != nil && a.server.Scheduler() != nil {
			a.server.Scheduler().TriggerAll()
		}
	})
	menu.Add("打开坚果云目录").OnClick(func(*application.Context) {
		if a.server != nil && a.server.Engine() != nil {
			root := a.server.Engine().GetNutstoreRoot()
			_ = exec.Command("explorer", root).Start()
		}
	})
	menu.Add("在默认浏览器中打开").OnClick(func(*application.Context) {
		_ = exec.Command("cmd", "/c", "start", a.serverURL).Start()
	})
	menu.AddSeparator()

	if IsRelease() {
		autostartItem := menu.AddCheckbox("开机自启", IsAutoStartEnabled())
		autostartItem.OnClick(func(*application.Context) {
			enabled := IsAutoStartEnabled()
			if err := SetAutoStart(!enabled); err != nil {
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

	if a.store != nil {
		_ = a.store.Close()
	}

	a.app.Quit()
}
