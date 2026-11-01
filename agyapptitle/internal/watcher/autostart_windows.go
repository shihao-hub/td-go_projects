package watcher

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"

	"language_projects/go_projects/agyapptitle/internal/buildinfo"
)

const autostartKeyName = "agyapptitle"

// GetAutostartStatus 检查当前是否已注册开机自启。
func GetAutostartStatus() (bool, string, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Run`, registry.QUERY_VALUE)
	if err != nil {
		return false, "", err
	}
	defer k.Close()

	val, _, err := k.GetStringValue(autostartKeyName)
	if err != nil {
		return false, "", nil
	}
	return true, val, nil
}

// EnableAutostart 注册开机自启动项，优先绑定无黑框正式版本 agyapptitlew.exe。
// 开发版（dev 构建）严格禁止写入注册表，遵循仓库开发环境隔离铁律。
func EnableAutostart() (string, error) {
	if !buildinfo.IsRelease() {
		return "", fmt.Errorf("dev_mode: 开发版禁止注册开机自启，避免污染系统注册表；请构建并使用 release 正式版")
	}

	exePath, err := os.Executable()
	if err != nil {
		return "", err
	}

	cleanExe := filepath.Clean(exePath)
	if strings.Contains(strings.ToLower(cleanExe), "go-build") || strings.HasPrefix(strings.ToLower(cleanExe), strings.ToLower(filepath.Clean(os.TempDir()))) {
		return "", fmt.Errorf("当前为临时构建可执行文件，拒绝注册开机自启")
	}

	dir := filepath.Dir(exePath)
	// 优先寻找同一目录下的无黑框正式版 agyapptitlew.exe
	targetExe := filepath.Join(dir, "agyapptitlew.exe")
	if _, err := os.Stat(targetExe); err != nil {
		targetExe = exePath
	}

	cmdLine := fmt.Sprintf(`"%s" watch`, targetExe)

	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Run`, registry.SET_VALUE)
	if err != nil {
		return "", err
	}
	defer k.Close()

	if err := k.SetStringValue(autostartKeyName, cmdLine); err != nil {
		return "", err
	}

	return cmdLine, nil
}

// DisableAutostart 注销开机自启动项。
func DisableAutostart() error {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Run`, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()

	return k.DeleteValue(autostartKeyName)
}
