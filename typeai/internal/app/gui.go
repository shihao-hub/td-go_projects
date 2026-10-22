package app

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"runtime"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"typeai/internal/api"
	"typeai/internal/appdata"
	"typeai/internal/browser"
	"typeai/internal/config"
	"typeai/internal/instance"
	"typeai/internal/service"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/w32"
)

const windowTitle = "typeai - AI 对话工作台"

type App struct {
	app       *application.App
	serverURL string
	http      *http.Server

	winMu sync.Mutex
	win   *application.WebviewWindow

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
	dir, err := appdata.Dir()
	if err != nil {
		return fmt.Errorf("初始化数据目录失败: %w", err)
	}
	cfg, err := config.Load(dir)
	if err != nil {
		return fmt.Errorf("读取配置失败: %w", err)
	}
	chat, err := service.NewChat(cfg, dir, time.Now())
	if err != nil {
		return fmt.Errorf("初始化会话失败: %w", err)
	}

	server := api.NewServer(api.Options{
		Chat:    chat,
		Config:  cfg,
		DataDir: dir,
		Version: Version(),
		Channel: Channel(),
	})

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("启动 HTTP 监听失败: %w", err)
	}
	serverURL := "http://" + listener.Addr().String()
	logf("WebUI 已启动: %s", serverURL)
	logf("数据目录: %s", dir)
	logf("版本: %s (build: %s, mode: %s, channel: %s)", Version(), BuildID(), BuildMode(), Channel())

	httpServer := &http.Server{Handler: server.Handler()}
	go func() {
		if err := httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			logf("HTTP 服务异常退出: %v", err)
		}
	}()

	var appInstance *App

	wailsApp := application.New(application.Options{
		Name:        appName,
		Description: "typeai - AI 对话桌面工作台",
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
		serverURL: serverURL,
		http:      httpServer,
		winState:  loadWindowState(),
	}

	appInstance.buildTray()
	if !hidden {
		appInstance.ensureMainWindow()
	}

	err = wailsApp.Run()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(shutdownCtx)
	return err
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
		BackgroundColour: application.NewRGB(23, 24, 26),
	}
	if state.HasPos {
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
	a.tray.SetTooltip("typeai - AI 对话工作台 " + Version())

	a.tray.OnClick(a.ensureMainWindow)
	a.tray.OnDoubleClick(a.ensureMainWindow)

	menu := a.app.NewMenu()
	menu.Add("打开工作台").OnClick(func(*application.Context) {
		a.ensureMainWindow()
	})
	menu.Add("在默认浏览器中打开").OnClick(func(*application.Context) {
		_ = browser.Open(a.serverURL)
	})
	menu.AddSeparator()

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
	if a.http != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = a.http.Shutdown(ctx)
		cancel()
	}
	a.app.Quit()
}

// OpenBrowser 用系统默认浏览器打开 URL，供 GUI 降级服务复用。
func OpenBrowser(url string) error {
	return browser.Open(url)
}
