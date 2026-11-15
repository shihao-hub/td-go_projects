package app

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"mail2feishu/internal/api"
	"mail2feishu/internal/buildinfo"
	"mail2feishu/internal/runtime"
)

const (
	appName     = "mail2feishu"
	windowTitle = "mail2feishu - QQ 邮箱推送飞书"
)

// GUI 持有 Wails 实例、托盘与后台服务。
type GUI struct {
	app       *application.App
	svc       *runtime.Runtime
	serverURL string
	logger    *log.Logger

	winMu   sync.Mutex
	win     *application.WebviewWindow
	tray    *application.SystemTray
	exiting atomic.Bool
}

// Launch 启动完整服务（IMAP 监听 + HTTP API + WebUI + 托盘）。
// port=0 时动态分配；hidden=true 时只留托盘，不打开主窗口。
func Launch(port int, hidden bool) error {
	dataDir, err := DataDir()
	if err != nil {
		return err
	}
	configPath, err := DefaultConfigPath()
	if err != nil {
		return err
	}

	release, err := AcquireInstance()
	if err != nil {
		return err
	}
	defer release()

	logger, closeLog, err := NewLogger(dataDir)
	if err != nil {
		return err
	}
	defer closeLog()

	svc, err := runtime.New(runtime.Options{
		ConfigPath: configPath,
		DataDir:    dataDir,
		Logger:     logger,
	})
	if err != nil {
		return fmt.Errorf("加载配置失败（%s）: %w", configPath, err)
	}
	defer svc.Close()
	if err := svc.Start(); err != nil {
		// 首次启动或配置缺失时仍打开界面，让用户在配置页补齐。
		logger.Printf("服务未启动（等待配置）: %v", err)
	}

	listener, err := api.Listen(port)
	if err != nil {
		return fmt.Errorf("启动本地 HTTP 服务失败: %w", err)
	}
	serverURL := api.URL(listener)
	go func() {
		if err := api.NewServer(svc).Serve(listener); err != nil {
			logger.Printf("HTTP 服务退出: %v", err)
		}
	}()
	logger.Printf("WebUI 已启动: %s", serverURL)

	gui := &GUI{
		svc:       svc,
		serverURL: serverURL,
		logger:    logger,
	}

	wailsApp := application.New(application.Options{
		Name:        appName,
		Description: windowTitle,
		Icon:        iconICO,
		Windows: application.WindowsOptions{
			DisableQuitOnLastWindowClosed: true,
			WebviewUserDataPath:           filepath.Join(dataDir, "webview2"),
		},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: singleInstanceID(),
			ExitCode: 0,
			OnSecondInstanceLaunch: func(application.SecondInstanceData) {
				gui.ensureWindow()
			},
		},
	})
	gui.app = wailsApp
	gui.setupTray()
	if !hidden {
		gui.ensureWindow()
	}
	return wailsApp.Run()
}

func singleInstanceID() string {
	if buildinfo.IsRelease() {
		return "shihao.langproj.mail2feishu.gui"
	}
	return "shihao.langproj.mail2feishu.gui.dev"
}

func (g *GUI) setupTray() {
	g.tray = g.app.SystemTray.New()
	if len(iconICO) > 0 {
		g.tray.SetIcon(iconICO)
	}
	g.tray.SetTooltip(windowTitle)
	g.tray.OnClick(g.ensureWindow)
	g.tray.OnDoubleClick(g.ensureWindow)

	menu := g.app.NewMenu()
	menu.Add("打开控制面板").OnClick(func(*application.Context) {
		g.ensureWindow()
	})
	menu.Add("测试飞书投递").OnClick(func(*application.Context) {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := g.svc.TestWebhook(ctx); err != nil {
				g.logf("测试投递失败: %v", err)
				return
			}
			g.logf("测试投递成功")
		}()
	})
	menu.Add("发送测试邮件").OnClick(func(*application.Context) {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := g.svc.SendTestMail(ctx); err != nil {
				g.logf("发送测试邮件失败: %v", err)
				return
			}
			g.logf("测试邮件已发送至 %s", g.svc.Config().IMAP.Username)
		}()
	})
	menu.Add("立即重连").OnClick(func(*application.Context) {
		go func() {
			if err := g.svc.Reconnect(); err != nil {
				g.logf("重连失败: %v", err)
				return
			}
			g.logf("已触发重连")
		}()
	})
	menu.AddSeparator()

	if buildinfo.IsRelease() {
		item := menu.AddCheckbox("开机自启", autostartEnabled(g.app))
		item.OnClick(func(*application.Context) {
			enabled := autostartEnabled(g.app)
			if err := setAutostart(g.app, !enabled); err != nil {
				g.logf("设置开机自启失败: %v", err)
				return
			}
			item.SetChecked(!enabled)
		})
	} else {
		disabled := menu.AddCheckbox("开机自启（开发版禁用）", false)
		disabled.SetEnabled(false)
	}

	menu.AddSeparator()
	menu.Add("彻底退出").OnClick(func(*application.Context) {
		go g.quit()
	})
	g.tray.SetMenu(menu)
}

func (g *GUI) ensureWindow() {
	go func() {
		if g.exiting.Load() {
			return
		}
		g.winMu.Lock()
		defer g.winMu.Unlock()

		if g.win != nil {
			g.win.Show()
			g.win.Focus()
			return
		}

		win := g.app.Window.NewWithOptions(application.WebviewWindowOptions{
			Title:            windowTitle,
			Width:            980,
			Height:           780,
			MinWidth:         780,
			MinHeight:        560,
			URL:              g.serverURL,
			BackgroundColour: application.NewRGB(243, 246, 243),
		})
		win.RegisterHook(events.Common.WindowClosing, func(*application.WindowEvent) {
			if g.exiting.Load() {
				return
			}
			g.winMu.Lock()
			if g.win == win {
				g.win = nil
			}
			g.winMu.Unlock()
		})
		g.win = win
		win.Show()
		win.Focus()
	}()
}

func (g *GUI) quit() {
	g.exiting.Store(true)
	g.svc.Stop()
	g.app.Quit()
}

func (g *GUI) logf(format string, args ...any) {
	g.logger.Printf(format, args...)
}
