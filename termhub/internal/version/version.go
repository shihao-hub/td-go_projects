// Package version termhub 构建信息（ldflags 注入；dev/prod 判定驱动数据目录分区）。
package version

import "sync/atomic"

// Version 版本号，build 时经 -ldflags 注入。
var Version = "v0.0.0-dev"

// BuildMode 构建形态：dev / release，build 时经 -ldflags 注入。
var BuildMode = "dev"

var isRelease atomic.Bool

// IsRelease 当前构建是否 release 形态。程序启动时由 LaunchGUI 调 Init 判定。
func IsRelease() bool { return isRelease.Load() }

// SetRelease 供启动装配判定后写入；测试也可直接调用。
func SetRelease(v bool) { isRelease.Store(v) }

// Channel 返回构建通道简称（数据目录分区名）。
func Channel() string {
	if IsRelease() {
		return "prod"
	}
	return "dev"
}
