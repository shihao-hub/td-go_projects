package app

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows/registry"
)

const runKeyPath = `Software\Microsoft\Windows\CurrentVersion\Run`

// SetAutoStart 设置开机自启
func SetAutoStart(enabled bool) error {
	if !IsRelease() {
		return fmt.Errorf("开发模式 (dev) 禁止注册开机自启动")
	}

	exePath, err := os.Executable()
	if err != nil {
		return err
	}

	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()

	if enabled {
		cmdVal := fmt.Sprintf(`"%s" --hidden`, exePath)
		return k.SetStringValue("NutVault", cmdVal)
	}
	_ = k.DeleteValue("NutVault")
	return nil
}

// IsAutoStartEnabled 检测是否已开启自启
func IsAutoStartEnabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()

	_, _, err = k.GetStringValue("NutVault")
	return err == nil
}
