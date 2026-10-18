package engine

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
	"winstartupmanager/internal/model"
)

// LocateInExplorer launches Windows Explorer and highlights the specified executable.
func LocateInExplorer(exePath string) error {
	cleaned := cleanExePath(exePath)
	if _, err := os.Stat(cleaned); err != nil {
		return fmt.Errorf("目标文件不存在: %s", cleaned)
	}

	cmd := exec.Command("explorer.exe", fmt.Sprintf("/select,%s", cleaned))
	return cmd.Start()
}

// LocateInRegistry writes LastKey to regedit configuration and launches regedit.exe.
func LocateInRegistry(source model.StartupSource, name string) error {
	regPath := RegistryPathForSource(source)
	fullKey := "Computer\\" + regPath

	k, _, err := registry.CreateKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Applets\Regedit`, registry.SET_VALUE)
	if err == nil {
		_ = k.SetStringValue("LastKey", fullKey)
		k.Close()
	}

	cmd := exec.Command("regedit.exe")
	return cmd.Start()
}

// IsProcessElevated checks if the current process is running with administrator privileges.
func IsProcessElevated() bool {
	var token windows.Token
	err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY, &token)
	if err != nil {
		return false
	}
	defer token.Close()

	return token.IsElevated()
}

// RestartAsAdmin relaunches the current process requesting UAC elevation (verb "runas").
func RestartAsAdmin() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}

	var args []string
	if len(os.Args) > 1 {
		args = os.Args[1:]
	}
	argsStr := strings.Join(args, " ")

	exePtr, err := syscall.UTF16PtrFromString(exe)
	if err != nil {
		return err
	}
	argsPtr, _ := syscall.UTF16PtrFromString(argsStr)
	verbPtr, _ := syscall.UTF16PtrFromString("runas")

	err = windows.ShellExecute(0, verbPtr, exePtr, argsPtr, nil, windows.SW_SHOWNORMAL)
	if err != nil {
		return fmt.Errorf("以管理员身份启动失败: %w", err)
	}

	// Exit current un-elevated instance
	os.Exit(0)
	return nil
}