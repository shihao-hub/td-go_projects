// gui.go：Wails v3 桌面壳装配——本地服务 + 托盘常驻 + 关窗重建。
package app

import (
	_ "embed"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/shihao-hub/termhub/internal/logx"
	"github.com/shihao-hub/termhub/internal/version"
)

//go:embed icon.ico
var iconICO []byte

const windowTitle = "termhub - 终端会话管理器"

// dataDir 返回 TERMHUB_DATA 覆盖目录，否则按数据隔离约束推导
// %APPDATA%\language_projects\termhub\<channel>；TERMHUB_DATA 仅在早期联调
// 或测试中显式设置，正式构建不依赖该变量。
func dataDir() string {
	if v := os.Getenv("TERMHUB_DATA"); v != "" {
		return v
	}
	appData := os.Getenv("APPDATA")
	if appData == "" {
		home, _ := os.UserHomeDir()
		appData = filepath.Join(home, ".language_projects")
	} else {
		appData = filepath.Join(appData, "language_projects")
	}
	dir := filepath.Join(appData, "termhub", version.Channel())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		// 数据目录建不出来不阻断壳启动，只让本次运行无落盘。
		return ""
	}
	return dir
}

// webviewDataPath WebView2 用户数据目录：与正常构建约定一致，落本仓数据根。
func webviewDataPath() string {
	if dir := dataDir(); dir != "" {
		return filepath.Join(dir, "webview")
	}
	return ""
}

// LaunchGUI 启动本地服务与 Wails 桌面壳。
func LaunchGUI(port int) error {
	version.SetRelease(version.BuildMode == "release")
	data := dataDir()
	logger := logx.Init(data)
	defer logx.Close()

	server := NewServer(data)
	httpServer, url, err := server.Start(port)
	if err != nil {
		return err
	}
	logger.Info("termhub starting",
		"version", version.Version,
		"channel", version.Channel(),
		"url", url,
	)
	statePath := windowStatePath(data)

	var (
		winMu sync.Mutex
		win   *application.WebviewWindow
		show  func()
	)

	wailsApp := application.New(application.Options{
		Name:        "termhub",
		Description: "termhub - Windows 终端会话与启动项生命周期管理器",
		Icon:        iconICO,
		Logger:      logger,
		Windows: application.WindowsOptions{
			WebviewUserDataPath: webviewDataPath(),
			// 关窗只销毁窗口，进程与终端会话继续驻留托盘。
			DisableQuitOnLastWindowClosed: true,
			AdditionalBrowserArgs: []string{
				"--disable-gpu",
				"--disable-gpu-compositing",
				"--disable-software-rasterizer",
				"--renderer-process-limit=1",
				"--disable-features=NetworkServiceOutOfProcess",
			},
		},
		// dev / prod 各自持锁，互不挤掉常驻实例。
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: "com.shihao-hub.termhub." + version.Channel(),
			OnSecondInstanceLaunch: func(application.SecondInstanceData) {
				logger.Info("second instance launch, showing window")
				if show != nil {
					show()
				}
			},
		},
	})

	// buildWindow 按记忆几何创建窗口，并挂接保存与屏外兜底逻辑。
	buildWindow := func() *application.WebviewWindow {
		state := loadWindowState(statePath)
		opts := application.WebviewWindowOptions{
			Title:            windowTitle,
			Width:            state.Width,
			Height:           state.Height,
			MinWidth:         minWindowWidth,
			MinHeight:        minWindowHeight,
			URL:              url + "/index.html",
			BackgroundColour: application.NewRGB(13, 17, 23),
			Windows: application.WindowsWindow{
				Theme: application.Dark,
			},
		}
		if state.HasPos {
			opts.InitialPosition = application.WindowXY
			opts.X = state.X
			opts.Y = state.Y
		}
		w := wailsApp.Window.NewWithOptions(opts)

		var (
			saveMu    sync.Mutex
			saveTimer *time.Timer
			closed    atomic.Bool
		)
		save := func() {
			if closed.Load() {
				return
			}
			current := loadWindowState(statePath)
			if w.IsMinimised() || w.IsFullscreen() {
				return
			}
			if w.IsMaximised() {
				// 最大化尺寸不能污染还原几何。
				current.Maximised = true
			} else {
				current.Width, current.Height = w.Size()
				current.X, current.Y = w.Position()
				current.HasPos = true
				current.Maximised = false
			}
			_ = writeWindowState(statePath, current)
		}
		scheduleSave := func() {
			saveMu.Lock()
			if saveTimer != nil {
				saveTimer.Stop()
			}
			saveTimer = time.AfterFunc(700*time.Millisecond, save)
			saveMu.Unlock()
		}

		w.OnWindowEvent(events.Common.WindowDidMove, func(*application.WindowEvent) { scheduleSave() })
		w.OnWindowEvent(events.Common.WindowDidResize, func(*application.WindowEvent) { scheduleSave() })
		w.OnWindowEvent(events.Common.WindowMaximise, func(*application.WindowEvent) { scheduleSave() })
		w.OnWindowEvent(events.Common.WindowUnMaximise, func(*application.WindowEvent) { scheduleSave() })
		w.OnWindowEvent(events.Common.WindowShow, func(*application.WindowEvent) {
			if loadWindowState(statePath).Maximised {
				w.Maximise()
				return
			}
			ensureWindowOnScreen(w, wailsApp.Screen.GetAll(), statePath)
		})

		w.RegisterHook(events.Common.WindowClosing, func(*application.WindowEvent) {
			// 放行关闭：销毁 WebView2 释放内存；同步落盘要在窗口销毁前完成。
			saveMu.Lock()
			if saveTimer != nil {
				saveTimer.Stop()
			}
			saveMu.Unlock()
			save()
			closed.Store(true)

			winMu.Lock()
			if win == w {
				win = nil
			}
			winMu.Unlock()

			go func() {
				time.Sleep(600 * time.Millisecond)
				runtime.GC()
				debug.FreeOSMemory()
			}()
		})

		return w
	}

	show = func() {
		winMu.Lock()
		w := win
		if w == nil {
			w = buildWindow()
			win = w
		}
		winMu.Unlock()

		if w.IsMinimised() {
			w.UnMinimise()
		}
		w.Show()
		w.Focus()
	}

	tray := wailsApp.SystemTray.New()
	tray.SetIcon(iconICO)
	tray.SetTooltip(windowTitle)

	trayMenu := wailsApp.NewMenu()
	trayMenu.Add("显示主窗口").OnClick(func(*application.Context) { show() })
	trayMenu.AddSeparator()
	trayMenu.Add("退出 termhub").OnClick(func(*application.Context) { wailsApp.Quit() })
	tray.SetMenu(trayMenu)
	tray.OnClick(show)
	tray.OnDoubleClick(show)

	show()

	// 真正退出时先终止所有会话，避免遗留 shell 子进程。
	wailsApp.OnShutdown(func() {
		server.Shutdown()
		logger.Info("termhub stopped")
	})

	err = wailsApp.Run()
	_ = httpServer.Close()
	return err
}

// ensureWindowOnScreen 外接屏拔掉后旧坐标可能让窗口彻底不可见；此时居中并回写。
func ensureWindowOnScreen(w *application.WebviewWindow, screens []*application.Screen, statePath string) {
	if w.IsMaximised() {
		return
	}
	areas := make([]windowArea, 0, len(screens))
	for _, screen := range screens {
		area := screen.WorkArea
		areas = append(areas, windowArea{X: area.X, Y: area.Y, Width: area.Width, Height: area.Height})
	}

	width, height := w.Size()
	x, y := w.Position()
	probe := windowState{Width: width, Height: height, X: x, Y: y, HasPos: true}
	if probe.visibleOn(areas) {
		return
	}

	w.Center()
	state := loadWindowState(statePath)
	state.Width, state.Height = w.Size()
	state.X, state.Y = w.Position()
	state.HasPos = true
	_ = writeWindowState(statePath, state)
}
