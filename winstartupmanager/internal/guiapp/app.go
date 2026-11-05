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
	"winstartupmanager/internal/logx"
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
	app   *application.App
	winMu sync.Mutex
	win   *application.WebviewWindow

	// ensureMu 串行化窗口的唤起/重建，防止托盘连点并发双建窗。
	ensureMu sync.Mutex

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
		Logger:      logx.SystemLogger(),
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
	logx.Infof("初始化 GUI 通道=%s 单实例ID=%s webview2=%s", env.ModeName(), env.SingleInstanceID(), webviewDataPath)

	a.buildTray()

	// 首窗与后续唤起统一走 ensureMainWindow（goroutine 异步建窗）：绝不在
	// Wails 事件回调或主线程同步建窗——WebView2 初始化需要消息泵继续运转，
	// 同步建窗会让消息泵卡死。--hidden 下完全不建窗，纯 Go 托盘常驻，
	// 托盘唤起时才首次建窗（家族同款）。
	if !opts.Hidden {
		a.ensureMainWindow()
	}

	if err := app.Run(); err != nil {
		panic(err)
	}
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

	win := a.app.Window.NewWithOptions(opts)
	a.win = win
	logx.Infof("主窗口已创建 width=%d height=%d hasPosition=%v maximised=%v hidden=%v",
		state.Width, state.Height, state.HasPos, state.Maximised, hidden)

	// 钩子闭包一律捕获局部 win：懒重建后 a.win 指向新窗，旧钩子若解引用
	// a.win 字段会错指新窗。
	win.RegisterHook(events.Common.WindowShow, func(*application.WindowEvent) {
		a.ensureWindowOnScreen(win)
		if a.currentWindowState().Maximised && !win.IsMaximised() {
			win.Maximise()
		}
	})

	for _, evt := range []events.WindowEventType{
		events.Common.WindowDidMove,
		events.Common.WindowDidResize,
		events.Common.WindowMaximise,
		events.Common.WindowUnMaximise,
	} {
		win.OnWindowEvent(evt, func(*application.WindowEvent) {
			a.scheduleSaveWindowState(win)
		})
	}

	win.RegisterHook(events.Common.WindowClosing, func(*application.WindowEvent) {
		// 窗口销毁后 Size() 会归零，必须在关闭路径上同步落盘一次。
		a.stopWindowStateTimer()
		a.saveWindowState(win)

		if a.exiting.Load() {
			return // 托盘「退出」路径放行全局退出，指针已由 quit() 清空
		}

		// 放行销毁：不调用 event.Cancel()，让 Wails 彻底销毁原生窗口并卸载
		// WebView2，Chromium 子进程全部退出；托盘唤起时再毫秒级懒重建。
		a.winMu.Lock()
		if a.win == win {
			a.win = nil
		}
		a.winMu.Unlock()

		go func() {
			time.Sleep(300 * time.Millisecond)
			runtime.GC()
			debug.FreeOSMemory()
		}()
	})
}

// ensureMainWindow 供 UI 回调（托盘点击/双击、第二实例唤醒）调用。
// 绝不在 Wails 事件回调里同步建窗——WebView2 初始化需要消息泵继续运转，
// 回调内同步建窗会卡死消息泵（窗口只建不显、托盘无响应），唤起一律异步。
func (a *App) ensureMainWindow() {
	go a.showMainWindow()
}

// showMainWindow 唤出或按需懒重建主窗口；ensureMu 串行化防托盘连点双建。
func (a *App) showMainWindow() {
	a.ensureMu.Lock()
	defer a.ensureMu.Unlock()

	if a.exiting.Load() {
		return
	}

	// 热态：窗口指针仍存活（GetByID 复核，悬垂指针顺手清理）。
	if win := a.aliveWindow(); win != nil {
		if win.IsMinimised() {
			win.UnMinimise()
		}
		win.Show()
		win.Focus()
		if a.currentWindowState().Maximised && !win.IsMaximised() {
			win.Maximise()
		}
		return
	}

	// 深度休眠态：窗口已被 X 销毁，winMu 内按需重建 WebView2。
	a.winMu.Lock()
	a.createWindowLocked(false)
	win := a.win
	a.winMu.Unlock()
	if win == nil {
		logx.Errorf("主窗口重建失败")
		return
	}

	win.Show()
	win.Focus()
	if a.currentWindowState().Maximised && !win.IsMaximised() {
		win.Maximise()
	}
}

// aliveWindow 返回当前存活的主窗口；已销毁时顺手清空悬垂引用。
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

func (a *App) quit() {
	logx.Infof("收到退出请求")
	a.exiting.Store(true)
	a.stopWindowStateTimer()

	// 锁内只取指针并清空，保存与关闭一律在锁外执行（家族同款）：避免持
	// winMu 调用 Wails API，也杜绝关闭钩子与 quit 的锁序耦合。
	a.winMu.Lock()
	win := a.win
	a.win = nil
	a.winMu.Unlock()

	if win != nil {
		// 窗口销毁后 Size() 会归零，必须在关闭前同步落盘。
		a.saveWindowState(win)
		win.Close()
	}

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
	if err := writeWindowState(state); err != nil {
		logx.Warnf("保存窗口状态失败: %v", err)
	}
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
