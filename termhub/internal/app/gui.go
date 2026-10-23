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

	// 自研单实例：互斥体占用即检查交接，唤出既有窗口后本进程退出。
	only, existingURL := acquireInstance(data, version.Channel())
	if !only {
		logger.Info("existing instance detected", "url", existingURL)
		if existingURL != "" && handoffToExisting(existingURL) {
			logger.Info("handoff done: existing instance prompted, exiting second instance")
			logx.Close()
			os.Exit(0)
		}
		// 互斥体被占但既有实例不可达：登记脏、拒绝第二实例避免蔽障。
		logger.Warn("existing instance unreachable; refusing second instance", "url", existingURL)
		return nil
	}

	server := NewServer(data, logger)
	logger.Info("assemble: session orchestration ready")
	httpServer, url, err := server.Start(port)
	if err != nil {
		return err
	}
	logger.Info("termhub starting",
		"version", version.Version,
		"channel", version.Channel(),
		"url", url,
	)
	if err := writeInstanceFile(data, url); err != nil {
		logger.Warn("write instance.json failed", "err", err.Error())
	}
	statePath := windowStatePath(data)

	var (
		winMu    sync.Mutex
		ensureMu sync.Mutex
		win      *application.WebviewWindow
		show     func()
	)

	logger.Info("assemble: creating wails app")
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
		// dev / prod 各自持锁，互不挤掉常驻实例（见 acquireInstance）。
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
		ensureMu.Lock()
		defer ensureMu.Unlock()

		winMu.Lock()
		w := win
		if w != nil {
			// 窗口可能被其它路径销毁而指针未及时清理，按 ID 复核存活。
			if _, exists := wailsApp.Window.GetByID(w.ID()); !exists {
				logger.Warn("main window gone, rebuilding", "id", w.ID())
				win = nil
				w = nil
			}
		}
		if w == nil {
			w = buildWindow()
			win = w
			logger.Info("main window created", "id", w.ID())
		}
		winMu.Unlock()

		if w.IsMinimised() {
			w.UnMinimise()
		}
		w.Show()
		w.Focus()
		logger.Info("main window shown", "id", w.ID())
	}

	logger.Info("assemble: creating system tray")
	tray := wailsApp.SystemTray.New()
	tray.SetIcon(iconICO)
	tray.SetTooltip(windowTitle)

	var quitting atomic.Bool
	trayMenu := wailsApp.NewMenu()
	trayMenu.Add("显示主窗口").OnClick(func(*application.Context) {
		logger.Info("tray menu: show")
		go show()
	})
	trayMenu.AddSeparator()
	trayMenu.Add("退出 termhub").OnClick(func(*application.Context) {
		logger.Info("tray menu: quit requested")
		go func() {
			dialog := wailsApp.Dialog.Question()
			dialog.SetTitle("退出 termhub")
			dialog.SetMessage("退出将终止全部终端会话，确定？")
			dialog.AddButton("退出").OnClick(func() {
				if !quitting.CompareAndSwap(false, true) {
					return
				}
				logger.Info("quit confirmed")
				// 先隐藏窗口再收尾：收尾期间即使有短暂延迟，也不会留下
				// 被 Windows 判定为「未响应」的窗口给用户看。
				winMu.Lock()
				w := win
				winMu.Unlock()
				if w != nil {
					if _, exists := wailsApp.Window.GetByID(w.ID()); exists {
						w.Hide()
					}
				}
				// 会话收尾（杀进程树、合并历史、落盘）耗时不可预测，必须离开
				// UI 主线程执行；否则 Wails 退出清理期间消息循环被占住，窗口
				// 会被 Windows 标记「未响应」。
				go func() {
					server.Shutdown()
					logger.Info("all sessions stopped")
					wailsApp.Quit()
				}()
			})
			dialog.AddButton("取消").SetAsCancel()
			dialog.Show()
		}()
	})
	tray.SetMenu(trayMenu)
	tray.OnClick(func() {
		logger.Info("tray left click")
		go show()
	})
	tray.OnDoubleClick(func() {
		logger.Info("tray double click")
		go show()
	})
	logger.Info("system tray ready")

	// 既有实例唤出路径：第二实例经 HTTP 触发主窗口重建。
	server.OnShow = func() {
		logger.Info("second-instance handoff: show")
		go show()
	}

	show()

	// 真正退出时先终止所有会话，避免遗留 shell 子进程。
	wailsApp.OnShutdown(func() {
		// 托盘正常退出路径已在后台完成收尾，这里只剩幂等兜底；仍占 UI 线程，
		// 所以要确保它足够快（Service.Shutdown 共享总超时，不随会话数叠加）。
		server.Shutdown()
		logger.Info("termhub stopped")
		logx.Close()
		// Wails v3 beta 在退出清理阶段可能因托盘/对话框句柄阻塞主 goroutine，
		// 使 OnShutdown 执行完后进程仍无法退出；这里强制收尾，保证用户始终能退出。
		os.Exit(0)
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
