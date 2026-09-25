// autostart.go：开机自启薄封装。
// Wails v3 beta.25 内置 app.Autostart（Windows 实现为 HKCU\…\CurrentVersion\Run
// 注册表值，与设计一致）；本文件只补 dev 期防呆：临时构建产物（go run /
// go-build 缓存）拒绝注册，避免重启后留下失效自启项（评审 R-8）。
package guiapp

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// autostartValueName 注册表值名 / 自启标识。
const autostartValueName = "glmquotawatch-gui"

// setAutostart 开/关自启：开启时注册 `"exe" --hidden`（登录后托盘静默运行）。
func setAutostart(app *application.App, enabled bool) error {
	if !enabled {
		return app.Autostart.Disable()
	}
	if err := devExecutableGuard(); err != nil {
		return err
	}
	return app.Autostart.EnableWithOptions(application.AutostartOptions{
		Identifier: autostartValueName,
		Arguments:  []string{"--hidden"},
	})
}

// autostartEnabled 查询自启注册状态（查询失败按未启用处理）。
func autostartEnabled(app *application.App) bool {
	v, err := app.Autostart.IsEnabled()
	return err == nil && v
}

// devExecutableGuard 拒绝注册临时构建产物：
// go run 产物位于系统 Temp 的 go-build* 目录，重启后必然失效。
func devExecutableGuard() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("获取可执行文件路径失败: %w", err)
	}
	resolved := filepath.Clean(exe)
	if strings.Contains(resolved, "go-build") ||
		strings.HasPrefix(strings.ToLower(resolved), strings.ToLower(filepath.Clean(os.TempDir()))) {
		return fmt.Errorf("dev_executable: 当前 exe 为临时构建产物（%s），不可注册开机自启；请改用 wails3 build 产物验证", resolved)
	}
	return nil
}
