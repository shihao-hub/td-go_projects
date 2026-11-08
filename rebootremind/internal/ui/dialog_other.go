//go:build !windows

package ui

import (
	"errors"

	"rebootremind/internal/model"
)

// ShowReminder 在非 Windows 平台不可用。
func ShowReminder(snapshot model.Snapshot) (Action, error) {
	return ActionDismissed, errors.New("rebootremind 仅支持 Windows")
}

// ConfirmRestart 在非 Windows 平台不可用。
func ConfirmRestart() (bool, error) {
	return false, errors.New("rebootremind 仅支持 Windows")
}
