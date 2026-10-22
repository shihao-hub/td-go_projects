package env

import (
	"strings"
)

var (
	// Version 由构建脚本通过 -ldflags -X 注入，dev 通道带 -dev 后缀。
	Version = "v0.1.0-dev"
	// Commit 由构建脚本注入短哈希。
	Commit = "unknown"
	// BuildMode 是 dev/release 的唯一真源，只允许构建脚本注入 "dev" 或 "release"。
	BuildMode = ""
)

const (
	ModeDev     = "dev"
	ModeRelease = "release"

	baseSingleInstanceID = "shihao.langproj.winstartupmanager"
)

// IsDev 以 BuildMode 为唯一判据；未注入时按 dev 处理（保证本地 go run 不影响正式通道）。
func IsDev() bool {
	return BuildMode != ModeRelease
}

// ModeName 返回归一化后的通道名，供日志与界面展示。
func ModeName() string {
	if IsDev() {
		return ModeDev
	}
	return ModeRelease
}

func SingleInstanceID() string {
	if IsDev() {
		return baseSingleInstanceID + ".dev"
	}
	return baseSingleInstanceID
}

func WindowTitle() string {
	title := "WinStartupManager - 开机自启可视化管理工具"
	if IsDev() {
		return title + " [DEV " + Version + "]"
	}
	return title
}

func AutostartAllowed() bool {
	return !IsDev()
}

// IsDirty 表示 dev 通道构建时工作区存在未提交改动。
func IsDirty() bool {
	return strings.HasSuffix(Version, "-dirty")
}
