//go:build windows

package app

import (
	"errors"

	"golang.org/x/sys/windows"

	"mail2feishu/internal/buildinfo"
)

// ErrAlreadyRunning 表示已有实例持有监听锁。
var ErrAlreadyRunning = errors.New("已有 mail2feishu 实例在运行（GUI 或 CLI），请先退出后再启动")

const instanceMutexBase = "shihao.langproj.mail2feishu.instance"

func instanceMutexName() string {
	if buildinfo.IsRelease() {
		return instanceMutexBase
	}
	return instanceMutexBase + ".dev"
}

// AcquireInstance 获取 GUI/CLI 共用的单监听锁，防止两个进程同时拉取邮件
// 造成重复推送。返回的释放函数应在进程退出前调用。
func AcquireInstance() (func(), error) {
	name, err := windows.UTF16PtrFromString(instanceMutexName())
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateMutex(nil, false, name)
	if err != nil {
		return nil, err
	}
	if windows.GetLastError() == windows.ERROR_ALREADY_EXISTS {
		_ = windows.CloseHandle(handle)
		return nil, ErrAlreadyRunning
	}
	return func() { _ = windows.CloseHandle(handle) }, nil
}
