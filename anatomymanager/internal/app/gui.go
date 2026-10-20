package app

import (
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"anatomymanager/internal/api"
	"anatomymanager/internal/storage"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/w32"
)

const (
	appName     = "anatomymanager"
	windowTitle = "Anatomy Manager - 精密机械图谱工作台"
)

type App struct {
	app       *application.App
	serverURL string

	winMu sync.Mutex
	win   *application.WebviewWindow

	ensureMu sync.Mutex
	tray     *application.SystemTray
	exiting  atomic.Bool
}

func LaunchGUI(dbPath string, defaultPort int, hidden bool) error {
	if dbPath == "" {
		dbPath = DefaultDBPath()
	}
	store, err := storage.NewStore(dbPath)
	if err != nil {
		return fmt.Errorf("打开数据库失败: %w", err)
	}

	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", defaultPort))
	if err != nil {
		listener, err = net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return fmt.Errorf("启动 HTTP 监听失败: %w", err)
		}
	}
	port := listener.Addr().(*net.TCPAddr).Port
	serverURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	server := api.NewServer(store, port)
	mux := server.Handler()
	httpServer := &http.Server{Handler: mux}
	go func() {
		_ = httpServer.Serve(listener)
	}()

	var appInstance *App

	// 单实例互斥 ID 按构建通道隔离，避免 dev 调试实例与常驻正式版互相挤掉。
	singleInstanceID := "shihao.langproj.anatomymanager.gui"
	if !IsRelease() {
		singleInstanceID += ".dev"
	}

	wailsApp := application.New(application.Options{
		Name:        appName,
		Description: "Anatomy Manager - 精密机械图谱桌面客户端",
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
	}

	appInstance.buildTray()
	if !hidden {
		appInstance.ensureMainWindow()
	}

	return wailsApp.Run()
}

func (a *App) buildWindowLocked(hidden bool) {
	win := a.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            windowTitle,
		Width:            1280,
		Height:           850,
		MinWidth:         900,
		MinHeight:        600,
		Hidden:           hidden,
		URL:              a.serverURL,
		BackgroundColour: application.NewRGB(22, 22, 28),
	})
	a.win = win

	// 注册窗口显示与激活钩子，HWND 创建就绪后即刻写入大小图标与窗口类属性
	win.RegisterHook(events.Windows.WindowShow, func(*application.WindowEvent) {
		a.applyWindowIcon(win)
	})
	win.RegisterHook(events.Windows.WindowActive, func(*application.WindowEvent) {
		a.applyWindowIcon(win)
	})

	win.RegisterHook(events.Common.WindowClosing, func(*application.WindowEvent) {
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
	win.Focus()
	a.applyWindowIcon(win)
}

func (a *App) buildTray() {
	a.tray = a.app.SystemTray.New()
	if len(iconICO) > 0 {
		a.tray.SetIcon(iconICO)
	}
	a.tray.SetTooltip("Anatomy Manager - 精密机械图谱")

	a.tray.OnClick(a.ensureMainWindow)
	a.tray.OnDoubleClick(a.ensureMainWindow)

	menu := a.app.NewMenu()
	menu.Add("打开工作台").OnClick(func(*application.Context) {
		a.ensureMainWindow()
	})
	menu.Add("在默认浏览器中打开").OnClick(func(*application.Context) {
		_ = openURL(a.serverURL)
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
