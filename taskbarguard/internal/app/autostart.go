package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"taskbarguard/internal/buildinfo"

	"github.com/wailsapp/wails/v3/pkg/application"
)

const autostartIdentifier = "TaskbarGuard"

func (a *App) autostartEnabled() bool {
	if a.app == nil || a.app.Autostart == nil {
		return false
	}
	value, err := a.app.Autostart.IsEnabled()
	return err == nil && value
}

func (a *App) setAutostart(enabled bool) error {
	if a.app == nil || a.app.Autostart == nil {
		return fmt.Errorf("autostart 服务未就绪")
	}
	if !enabled {
		return a.app.Autostart.Disable()
	}
	if !buildinfo.IsRelease() {
		return fmt.Errorf("dev_mode: 开发版禁止注册开机自启，避免污染系统注册表")
	}
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("获取可执行文件路径失败: %w", err)
	}
	cleanExecutable := filepath.Clean(executable)
	if strings.Contains(cleanExecutable, "go-build") || strings.HasPrefix(strings.ToLower(cleanExecutable), strings.ToLower(filepath.Clean(os.TempDir()))) {
		return fmt.Errorf("当前为临时构建可执行文件，拒绝注册开机自启")
	}

	return a.app.Autostart.EnableWithOptions(application.AutostartOptions{
		Identifier: autostartIdentifier,
		Arguments:  []string{"--hidden"},
	})
}
