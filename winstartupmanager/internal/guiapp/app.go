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
}

func Run(opts RunOptions) {
	a := &App{icon: opts.Icon}
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

func (a *App) createWindowLocked(hidden bool) {
	a.win = a.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            env.WindowTitle(),
		Width:            1040,
		Height:           680,
		MinWidth:         820,
		MinHeight:        520,
		Hidden:           hidden,
		URL:              "/",
		BackgroundColour: application.NewRGB(248, 250, 252),
	})

	a.win.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
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
}

func (a *App) quit() {
	a.exiting.Store(true)
	a.winMu.Lock()
	if a.win != nil {
		a.win.Close()
	}
	a.win = nil
	a.winMu.Unlock()
	a.app.Quit()
}
