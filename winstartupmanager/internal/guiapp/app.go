package guiapp

import (
	"embed"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"winstartupmanager/internal/env"
	"winstartupmanager/internal/store"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

type RunOptions struct {
	Hidden bool
	Assets embed.FS
	Icon   []byte
}

type App struct {
	app     *application.App
	winMu   sync.Mutex
	win     *application.WebviewWindow
	exiting atomic.Bool
	tray    *application.SystemTray
	icon    []byte

	stateMu    sync.Mutex
	stateTimer *time.Timer
	winState   windowState
}

func Run(opts RunOptions) {
	a := &App{icon: opts.Icon, winState: loadWindowState()}
	service := NewStartupAppService(a)

	userDataDir, _ := store.DataDir()
	var webviewDataPath string
	if userDataDir != "" {
		webviewDataPath = filepath.Join(userDataDir, "webview2")
	}

	app := application.New(application.Options{
		Name:        "winstartupmanager",
		Description: "Windows 开机自启可视化管理工具",
		Services: []application.Service{
			application.NewService(service),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(opts.Assets),
		},
		Windows: application.WindowsOptions{
			DisableQuitOnLastWindowClosed: true,
			WebviewUserDataPath:           webviewDataPath,
			AdditionalBrowserArgs: []string{
				"--disable-gpu",
				"--disable-gpu-compositing",
				"--disable-software-rasterizer",
				"--renderer-process-limit=1",
				"--disable-features=NetworkServiceOutOfProcess",
			},
		},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: env.SingleInstanceID(),
			ExitCode: 0,
			OnSecondInstanceLaunch: func(data application.SecondInstanceData) {
				a.ensureMainWindow()
			},
		},
	})

	a.app = app

	// Always pre-create window before app.Run() (hidden if started with --hidden)
	// This guarantees WebView2 environment is properly initialized and avoids
	// runtime creation deadlocks on Windows message loops.
	a.buildWindow(opts.Hidden)
	a.buildTray()

	if err := app.Run(); err != nil {
		panic(err)
	}
}

func (a *App) buildWindow(hidden bool) {
	a.winMu.Lock()
	defer a.winMu.Unlock()
	a.createWindowLocked(hidden)
}

// createWindowLocked 按持久化几何建窗，并接入几何采集与屏幕可见性兜底。
func (a *App) createWindowLocked(hidden bool) {
	state := a.currentWindowState()

	opts := application.WebviewWindowOptions{
		Title:            env.WindowTitle(),
		Width:            state.Width,
		Height:           state.Height,
		MinWidth:         minWindowWidth,
		MinHeight:        minWindowHeight,
		Hidden:           hidden,
		URL:              "/",
		BackgroundColour: application.NewRGB(248, 250, 252),
	}
	if state.HasPos {
		opts.InitialPosition = application.WindowXY
		opts.X = state.X
		opts.Y = state.Y
	}

	a.win = a.app.Window.NewWithOptions(opts)

	a.win.RegisterHook(events.Common.WindowShow, func(event *application.WindowEvent) {
		a.ensureWindowOnScreen(a.win)
		if a.currentWindowState().Maximised && !a.win.IsMaximised() {
			a.win.Maximise()
		}
	})

	for _, evt := range []events.WindowEventType{
		events.Common.WindowDidMove,
		events.Common.WindowDidResize,
		events.Common.WindowMaximise,
		events.Common.WindowUnMaximise,
	} {
		a.win.OnWindowEvent(evt, func(event *application.WindowEvent) {
			a.scheduleSaveWindowState(a.win)
		})
	}

	a.win.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		// 窗口销毁后 Size() 会归零，必须在关闭路径上同步落盘一次。
		a.stopWindowStateTimer()
		a.saveWindowState(a.win)

		if a.exiting.Load() {
			return
		}
		// Cancel destruction so window can be summoned back instantly from tray
		event.Cancel()
		a.win.Hide()

		go func() {
			time.Sleep(300 * time.Millisecond)
			runtime.GC()
			debug.FreeOSMemory()
		}()
	})
}

func (a *App) ensureMainWindow() {
	a.winMu.Lock()
	defer a.winMu.Unlock()

	if a.win == nil {
		return
	}

	if a.win.IsMinimised() {
		a.win.UnMinimise()
	}
	a.win.Show()
	a.win.Focus()
	if a.currentWindowState().Maximised && !a.win.IsMaximised() {
		a.win.Maximise()
	}
}

func (a *App) quit() {
	a.exiting.Store(true)
	a.stopWindowStateTimer()
	a.winMu.Lock()
	if a.win != nil {
		a.saveWindowState(a.win)
		a.win.Close()
	}
	a.win = nil
	a.winMu.Unlock()
	a.app.Quit()
}

func (a *App) currentWindowState() windowState {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	return a.winState
}

func (a *App) setWindowState(state windowState) {
	a.stateMu.Lock()
	a.winState = state
	a.stateMu.Unlock()
}

// scheduleSaveWindowState 用 700ms 防抖吸收拖拽/缩放产生的高频事件。
func (a *App) scheduleSaveWindowState(win *application.WebviewWindow) {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	if a.stateTimer != nil {
		a.stateTimer.Stop()
	}
	a.stateTimer = time.AfterFunc(windowStateSaveDelay, func() {
		a.stateMu.Lock()
		a.stateTimer = nil
		a.stateMu.Unlock()
		a.saveWindowState(win)
	})
}

func (a *App) stopWindowStateTimer() {
	a.stateMu.Lock()
	if a.stateTimer != nil {
		a.stateTimer.Stop()
		a.stateTimer = nil
	}
	a.stateMu.Unlock()
}

// saveWindowState 采集当前几何；最大化只翻标记，最小化/全屏直接跳过。
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
		w, h := win.Size()
		if w < minWindowWidth || h < minWindowHeight {
			return
		}
		x, y := win.Position()
		state.Width, state.Height = w, h
		state.X, state.Y, state.HasPos = x, y, true
		state.Maximised = false
	}

	if state == a.currentWindowState() {
		return
	}
	a.setWindowState(state)
	_ = writeWindowState(state)
}

// ensureWindowOnScreen 在窗口落到已拔掉的外接屏时拉回主屏。
func (a *App) ensureWindowOnScreen(win *application.WebviewWindow) {
	if win == nil {
		return
	}
	screens := a.app.Screen.GetAll()
	if len(screens) == 0 {
		// Run() 之前屏幕缓存可能尚未就绪，不能据此判定"不可见"。
		return
	}

	x, y := win.Position()
	w, h := win.Size()
	cx, cy := x+w/2, y+h/2
	for _, s := range screens {
		area := s.WorkArea
		if cx >= area.X && cx < area.X+area.Width && cy >= area.Y && cy < area.Y+area.Height {
			return
		}
	}

	win.Center()
	a.scheduleSaveWindowState(win)
}
