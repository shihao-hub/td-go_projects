package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

type App struct {
	app        *application.App
	daemon     *DaemonManager
	serverURL  string
	winMu      sync.Mutex
	win        *application.WebviewWindow
	tray       *application.SystemTray
	exiting    atomic.Bool
	statusItem *application.MenuItem
}

const (
	defaultPort      = 8765
	singleInstanceID = "shihao.langproj.docsearch-gui"
	windowTitle      = "docsearch - 本地语义文档检索"
)

func main() {
	port := flag.Int("port", defaultPort, "docsearch 服务监听端口")
	hidden := flag.Bool("hidden", false, "开机静默启动（仅常驻托盘，不弹窗）")
	flag.Parse()

	serverURL := fmt.Sprintf("http://127.0.0.1:%d", *port)
	daemon := NewDaemonManager(serverURL)

	// 提前探测并异步确保 docsearch 后台服务就绪
	go func() {
		if err := daemon.EnsureRunning(); err != nil {
			fmt.Fprintf(os.Stderr, "[docsearch-gui] 启动后台服务异常: %v\n", err)
		}
	}()

	var appInstance *App

	// 计算 WebView2 用户数据缓存路径，避免散落根目录
	var webviewDataPath string
	if appData, err := os.UserConfigDir(); err == nil && appData != "" {
		webviewDataPath = filepath.Join(appData, "language_projects", "docsearch", "webview2")
	}

	wailsApp := application.New(application.Options{
		Name:        "docsearch-gui",
		Description: "docsearch 本地语义文档检索桌面托盘壳",
		Windows: application.WindowsOptions{
			// 核心 1：所有窗口关闭后，托盘与后台消息泵继续存活，绝不退出进程
			DisableQuitOnLastWindowClosed: true,
			WebviewUserDataPath:           webviewDataPath,

			// 核心 2：Chromium 启动参数深度剪枝（消灭独立 GPU 进程与网络隔离进程，常驻立省 160MB）
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
		daemon:    daemon,
		serverURL: serverURL,
	}

	// 托盘常驻初始化
	appInstance.buildTray()

	// 若非静默启动，默认直接开窗
	if !*hidden {
		appInstance.ensureMainWindow()
	}

	// 启动定期刷新托盘状态的协程
	go appInstance.loopUpdateStatus()

	if err := wailsApp.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "[docsearch-gui] 运行异常: %v\n", err)
		os.Exit(1)
	}
}

// createWindowLocked 创建主窗口并挂载关闭释放钩子（调用方需持有 winMu）
func (a *App) createWindowLocked(hidden bool) {
	a.win = a.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            windowTitle,
		Width:            1020,
		Height:           680,
		MinWidth:         760,
		MinHeight:        520,
		Hidden:           hidden,
		URL:              a.serverURL,
		BackgroundColour: application.NewRGB(30, 30, 36), // #1e1e24 与 WebUI 暗黑背景无缝衔接
	})

	// 核心生命周期分流 1：点击右上角 X 窗口关闭
	// 需求：关闭但在托盘时，webview 不渲染
	a.win.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		if a.exiting.Load() {
			return // 用户从托盘菜单点击“退出”时，放行全局退出
		}

		// 核心关键：不调用 event.Cancel()！放行让 Wails 彻底销毁原生窗口并卸载 WebView2
		// 这将导致 Chromium 的 6 个子进程完全退出，DOM / V8 / 渲染管线彻底清空，完全停止渲染
		a.winMu.Lock()
		a.win = nil
		a.winMu.Unlock()

		// 异步触发垃圾回收与物理内存归还操作系统
		go func() {
			time.Sleep(250 * time.Millisecond)
			runtime.GC()
			debug.FreeOSMemory()
		}()
	})

	// 核心生命周期分流 2：点击最小化按钮 (-)
	// 需求：最小化时渲染
	// 说明：用户在标题栏点击 "-" 最小化按钮走的是 Windows 原生的 SC_MINIMIZE，
	// 不会触发 WindowClosing 钩子。窗口缩在任务栏，WebView2 渲染管线与 JS 状态完全保留！
}

// isWindowAlive 判断当前主窗口实例是否存在且未被销毁
func (a *App) isWindowAlive() bool {
	if a.win == nil {
		return false
	}
	_, exists := a.app.Window.GetByID(a.win.ID())
	return exists
}

// ensureMainWindow 唤出或按需瞬间重建主窗口
func (a *App) ensureMainWindow() {
	a.winMu.Lock()
	defer a.winMu.Unlock()

	// 确保后台 Python 服务已在运行
	go func() {
		_ = a.daemon.EnsureRunning()
	}()

	// 1. 若窗口仍在内存中（例如用户点击任务栏最小化，仍在热态待命）
	if a.isWindowAlive() {
		if a.win.IsMinimised() {
			a.win.UnMinimise()
		}
		a.win.Show()
		a.win.Focus()
		return
	}

	// 2. 若窗口已被点“X”关闭销毁（深度休眠态），则毫秒级按需重建 WebView2
	a.createWindowLocked(false)
	if a.win != nil {
		a.win.Show()
		a.win.Focus()
	}
}

// buildTray 初始化系统托盘与右键菜单
func (a *App) buildTray() {
	a.tray = a.app.SystemTray.New()
	if len(iconICO) > 0 {
		a.tray.SetIcon(iconICO)
	}
	a.tray.SetTooltip("docsearch - 本地语义文档检索")

	// 托盘单击与双击均支持一键呼出/隐藏主窗口
	a.tray.OnClick(func() {
		a.ensureMainWindow()
	})
	a.tray.OnDoubleClick(func() {
		a.ensureMainWindow()
	})

	menu := a.app.NewMenu()
	menu.Add("打开搜索窗口").OnClick(func(*application.Context) {
		a.ensureMainWindow()
	})

	menu.Add("增量重建索引").OnClick(func(*application.Context) {
		go func() {
			a.tray.SetTooltip("docsearch: 正在扫描并构建索引...")
			err := a.daemon.ReindexAll(false)
			if err != nil {
				a.tray.SetTooltip("docsearch: 重建索引失败")
				time.Sleep(3 * time.Second)
			} else {
				a.tray.SetTooltip("docsearch: 索引构建完成！")
				time.Sleep(2 * time.Second)
			}
			a.updateStatusText()
		}()
	})

	menu.Add("在默认浏览器中打开").OnClick(func(*application.Context) {
		_ = openURL(a.serverURL)
	})

	menu.AddSeparator()
	a.statusItem = menu.Add("状态: 正在连接后台...")
	a.statusItem.SetEnabled(false)

	menu.AddSeparator()
	menu.Add("彻底退出").OnClick(func(*application.Context) {
		a.quit()
	})

	a.tray.SetMenu(menu)
}

// loopUpdateStatus 定期探活并更新托盘 Tooltip 与状态菜单
func (a *App) loopUpdateStatus() {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	// 启动后立即更新一次
	time.Sleep(1 * time.Second)
	a.updateStatusText()

	for range ticker.C {
		if a.exiting.Load() {
			return
		}
		a.updateStatusText()
	}
}

func (a *App) updateStatusText() {
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

	label := fmt.Sprintf("状态: 已索引 %d 个文件 (%d 片段)", st.TotalFiles, st.TotalChunks)
	if a.statusItem != nil {
		a.statusItem.SetLabel(label)
	}
	if a.tray != nil {
		a.tray.SetTooltip(fmt.Sprintf("docsearch: %d 文件 / %d 片段\n左键打开搜索", st.TotalFiles, st.TotalChunks))
	}
}

// quit 彻底退出应用
func (a *App) quit() {
	a.exiting.Store(true)

	// 关闭主窗口
	a.winMu.Lock()
	if a.win != nil && a.isWindowAlive() {
		a.win.Close()
	}
	a.win = nil
	a.winMu.Unlock()

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
