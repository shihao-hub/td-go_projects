// gui.go：Wails v3 桌面壳装配——本地服务 + 主窗口加载 WebUI。
package app

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v3/pkg/application"

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
	server := NewServer(dataDir())
	httpServer, url, err := server.Start(port)
	if err != nil {
		return err
	}

	wailsApp := application.New(application.Options{
		Name:        "termhub",
		Description: "termhub - Windows 终端会话与启动项生命周期管理器",
		Icon:        iconICO,
		Windows: application.WindowsOptions{
			WebviewUserDataPath: webviewDataPath(),
			AdditionalBrowserArgs: []string{
				"--disable-gpu",
				"--disable-gpu-compositing",
				"--disable-software-rasterizer",
				"--renderer-process-limit=1",
				"--disable-features=NetworkServiceOutOfProcess",
			},
		},
	})

	win := wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            windowTitle,
		Width:            1080,
		Height:           720,
		MinWidth:         720,
		MinHeight:        480,
		URL:              url + "/index.html",
		BackgroundColour: application.NewRGB(244, 246, 244),
	})
	_ = win

	err = wailsApp.Run()
	_ = httpServer.Close()
	return err
}

// ensure fmt keep for diagnostics in early dev.
var _ = fmt.Sprintf
