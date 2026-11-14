package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"

	"mail2feishu/internal/buildinfo"
)

const autostartIdentifier = "mail2feishu"

func autostartEnabled(app *application.App) bool {
	if app == nil || app.Autostart == nil {
		return false
	}
	enabled, err := app.Autostart.IsEnabled()
	return err == nil && enabled
}

// setAutostart 仅允许正式版写入注册表；开发版直接拒绝，避免污染系统。
func setAutostart(app *application.App, enabled bool) error {
	if app == nil || app.Autostart == nil {
		return fmt.Errorf("autostart 服务未就绪")
	}
	if !enabled {
		return app.Autostart.Disable()
	}
	if !buildinfo.IsRelease() {
		return fmt.Errorf("开发版禁止注册开机自启，请使用 release 构建")
	}

	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("获取可执行文件路径失败: %w", err)
	}
	clean := filepath.Clean(executable)
	if strings.Contains(clean, "go-build") ||
		strings.HasPrefix(strings.ToLower(clean), strings.ToLower(filepath.Clean(os.TempDir()))) {
		return fmt.Errorf("临时构建产物禁止注册开机自启")
	}

	return app.Autostart.EnableWithOptions(application.AutostartOptions{
		Identifier: autostartIdentifier,
		Arguments:  []string{"--hidden"},
	})
}
