package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

const (
	defaultPort = 8765
	appName     = "docsearch-gui"
	windowTitle = "docsearch - 本地语义文档检索"
)

type App struct {
	app       *application.App
	daemon    *DaemonManager
	serverURL string

	winMu sync.Mutex
	win   *application.WebviewWindow

	// ensureMu 串行化窗口的创建/唤出，避免连点托盘时并发建窗。
	ensureMu sync.Mutex

	tray        *application.SystemTray
	statusItem  *application.MenuItem
	reindexItem *application.MenuItem

	exiting        atomic.Bool
	reindexBusy    atomic.Bool
	statusHoldTill atomic.Int64
}

func main() {
	defer Recover()

	port := flag.Int("port", defaultPort, "docsearch 服务监听端口")
	hidden := flag.Bool("hidden", false, "开机静默启动（仅常驻托盘，不弹窗）")
	flag.Parse()

	daemon := NewDaemonManager(*port)

	var appInstance *App

	wailsApp := application.New(application.Options{
		Logger:      wailsLogger(),
		Name:        appName,
		Description: "docsearch 本地语义文档检索桌面托盘壳",
		Windows: application.WindowsOptions{
			// 所有窗口关闭后，托盘与后台消息泵继续存活，绝不退出进程
			DisableQuitOnLastWindowClosed: true,
			WebviewUserDataPath:           webviewDataPath(),

			// Chromium 启动参数深度剪枝（消灭独立 GPU 进程与网络隔离进程，常驻立省 160MB）
			AdditionalBrowserArgs: []string{
				"--disable-gpu",
				"--disable-gpu-compositing",
				"--disable-software-rasterizer",
				"--renderer-process-limit=1",
				"--disable-features=NetworkServiceOutOfProcess",
			},
		},
		SingleInstance: &application.SingleInstanceOptions{
			// 单实例标识带上端口，避免不同端口的实例互相吞并
			UniqueID: fmt.Sprintf("shihao.langproj.docsearch-gui.%d", *port),
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
		daemon:    daemon,
		serverURL: daemon.URL(),
	}

	appInstance.buildTray()

	// 后台服务异步拉起；就绪后若主窗口已打开则自动刷新，避免停在后端未就绪的错误页
	go appInstance.kickDaemon()

	if !*hidden {
		appInstance.ensureMainWindow()
	}

	go appInstance.loopUpdateStatus()

	if err := wailsApp.Run(); err != nil {
		logf("运行异常: %v", err)
		os.Exit(1)
	}
}

// buildWindowLocked 创建主窗口并挂载关闭释放钩子（调用方需持有 winMu）。
func (a *App) buildWindowLocked(hidden bool) {
	win := a.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            windowTitle,
		Width:            1020,
		Height:           680,
		MinWidth:         760,
		MinHeight:        520,
		Hidden:           hidden,
		URL:              a.serverURL,
		BackgroundColour: application.NewRGB(30, 30, 36), // #1e1e24 与 WebUI 暗黑背景无缝衔接
	})
	a.win = win

	// 关闭 (X) 分流：不调用 event.Cancel()，放行 Wails 彻底销毁原生窗口并卸载 WebView2，
	// 让 Chromium 子进程全部退出，完全停止渲染；随后托盘点击时再按需重建。
	win.RegisterHook(events.Common.WindowClosing, func(*application.WindowEvent) {
		if a.exiting.Load() {
			return // 托盘“彻底退出”时放行全局退出
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

// aliveWindow 返回当前存活的主窗口；指针已销毁时顺手清空，避免使用失效句柄。
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

// ensureMainWindow 供 UI 回调（托盘点击、第二实例唤醒）调用。
// 关键：绝不在 Wails 主线程/事件回调内创建或销毁窗口——WebView2 初始化需要消息泵继续
// 运转，在回调里同步建窗会让消息泵卡死，表现为“窗口只建不显、托盘从此无响应”。
func (a *App) ensureMainWindow() {
	go a.showMainWindow()
}

// showMainWindow 唤出或按需重建主窗口。
func (a *App) showMainWindow() {
	a.ensureMu.Lock()
	defer a.ensureMu.Unlock()

	if a.exiting.Load() {
		return
	}

	// 1. 窗口仍在内存中（例如用户点击任务栏最小化，仍在热态待命）
	if win := a.aliveWindow(); win != nil {
		if win.IsMinimised() {
			win.UnMinimise()
		}
		win.Show()
		win.Focus()
		return
	}

	// 2. 窗口已被点“X”关闭销毁（深度休眠态），则按需重建 WebView2
	needReload := !a.daemon.IsAlive()

	a.winMu.Lock()
	a.buildWindowLocked(false)
	win := a.win
	a.winMu.Unlock()
	if win == nil {
		return
	}

	win.Show()
	win.Focus()

	// 建窗时后端还没就绪：窗口会先显示连接错误页，等服务起来后自动刷新
	if needReload {
		go a.ensureDaemonThenReload()
	}
}

// kickDaemon 启动时拉起后台服务，并在就绪后刷新已打开的窗口。
func (a *App) kickDaemon() {
	a.ensureDaemonThenReload()
}

// ensureDaemonThenReload 幂等地确保后台服务就绪，随后刷新当前窗口。
func (a *App) ensureDaemonThenReload() {
	if err := a.daemon.EnsureRunning(); err != nil {
		logf("后台服务启动异常: %v", err)
		return
	}
	if win := a.aliveWindow(); win != nil {
		win.Reload()
	}
}

// buildTray 初始化系统托盘与右键菜单。
func (a *App) buildTray() {
	a.tray = a.app.SystemTray.New()
	if len(iconICO) > 0 {
		a.tray.SetIcon(iconICO)
	}
	a.tray.SetTooltip("docsearch - 本地语义文档检索")

	// 托盘单击与双击均支持一键呼出/隐藏主窗口
	a.tray.OnClick(a.ensureMainWindow)
	a.tray.OnDoubleClick(a.ensureMainWindow)

	menu := a.app.NewMenu()
	menu.Add("打开搜索窗口").OnClick(func(*application.Context) {
		a.ensureMainWindow()
	})

	a.reindexItem = menu.Add("增量重建索引")
	a.reindexItem.OnClick(func(*application.Context) {
		a.toggleReindex()
	})

	menu.Add("在默认浏览器中打开").OnClick(func(*application.Context) {
		if err := openURL(a.serverURL); err != nil {
			logf("打开浏览器失败: %v", err)
		}
	})

	menu.AddSeparator()
	a.statusItem = menu.Add("状态: 正在连接后台...")
	a.statusItem.SetEnabled(false)

	menu.AddSeparator()
	menu.Add("彻底退出").OnClick(func(*application.Context) {
		go a.quit()
	})

	a.tray.SetMenu(menu)
}

// toggleReindex 触发一次增量重建索引；运行期间禁用菜单项，杜绝并发重复触发。
func (a *App) toggleReindex() {
	if !a.reindexBusy.CompareAndSwap(false, true) {
		return
	}
	if a.reindexItem != nil {
		a.reindexItem.SetEnabled(false)
	}

	go func() {
		defer func() {
			a.reindexBusy.Store(false)
			if a.reindexItem != nil {
				a.reindexItem.SetEnabled(true)
			}
		}()

		a.setTemporaryStatus("docsearch: 正在扫描并构建索引...", 30*time.Minute)
		if err := a.daemon.ReindexAll(false); err != nil {
			logf("重建索引失败: %v", err)
			a.setTemporaryStatus("docsearch: 重建索引失败（详见日志）", 6*time.Second)
			return
		}
		a.setTemporaryStatus("docsearch: 索引构建完成！", 4*time.Second)
	}()
}

// loopUpdateStatus 定期探活并更新托盘 Tooltip 与状态菜单。
func (a *App) loopUpdateStatus() {
	time.Sleep(time.Second)
	a.updateStatusText()

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		if a.exiting.Load() {
			return
		}
		a.updateStatusText()
	}
}

// setTemporaryStatus 展示一段临时状态文案，在 hold 期间不被周期探活覆盖。
func (a *App) setTemporaryStatus(text string, hold time.Duration) {
	a.statusHoldTill.Store(time.Now().Add(hold).UnixNano())
	if a.tray != nil {
		a.tray.SetTooltip(text)
	}
}

func (a *App) updateStatusText() {
	if time.Now().UnixNano() < a.statusHoldTill.Load() {
		return // 临时提示（建索引等）优先，期间不覆盖
	}

	st, err := a.daemon.GetStatus()
	if err != nil {
		if a.statusItem != nil {
			a.statusItem.SetLabel("状态: 后台未连接")
		}
		if a.tray != nil {
			a.tray.SetTooltip("docsearch: 后台未运行")
		}
		return
	}

	if a.statusItem != nil {
		a.statusItem.SetLabel(fmt.Sprintf("状态: 已索引 %d 个文件 (%d 片段)", st.TotalFiles, st.TotalChunks))
	}
	if a.tray != nil {
		a.tray.SetTooltip(fmt.Sprintf("docsearch: %d 文件 / %d 片段\n左键打开搜索", st.TotalFiles, st.TotalChunks))
	}
}

// quit 彻底退出应用。
func (a *App) quit() {
	a.exiting.Store(true)

	a.winMu.Lock()
	win := a.win
	a.win = nil
	a.winMu.Unlock()

	if win != nil {
		win.Close()
	}

	// 停止由我们拉起的 Python 守护进程
	a.daemon.Stop()

	// 退出 Wails 消息泵
	a.app.Quit()
}

func openURL(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}
