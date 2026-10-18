package env

import (
	"strings"
)

var (
	Version = "v0.1.0-dev"
	Commit  = "unknown"
)

const baseSingleInstanceID = "shihao.langproj.winstartupmanager"

func IsDev() bool {
	return strings.Contains(Version, "-dev")
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