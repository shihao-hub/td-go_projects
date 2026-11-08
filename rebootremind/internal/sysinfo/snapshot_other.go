//go:build !windows

package sysinfo

import (
	"errors"

	"rebootremind/internal/model"
)

// Collect 在非 Windows 平台直接报错。
func Collect(topN int) (model.Snapshot, error) {
	return model.Snapshot{}, errors.New("rebootremind 仅支持 Windows")
}
