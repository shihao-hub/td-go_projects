// Package appdata 是 agyquota 运行时数据目录的单点解析（仓库强约束）：
// 所有自产文件（地址文件、日志、缓存、静默副本）只允许落在
// %APPDATA%\language_projects\agyquota\；取不到 APPDATA 时回退
// ~/.language_projects/agyquota/。写前由调用方创建完整目录链。
package appdata

import (
	"os"
	"path/filepath"
)

const (
	// ProjectName 是数据目录中的项目名。
	ProjectName = "agyquota"
	// AddressFileName 是 daemon 地址文件名。
	AddressFileName = "daemon.json"
	// LogFileName 是 daemon 日志文件名（轮转后为 .1 后缀）。
	LogFileName = "daemon.log"
)

// Dir 返回本项目运行时数据目录；解析失败返回空串。
func Dir() string {
	if appdata := os.Getenv("APPDATA"); appdata != "" {
		return filepath.Join(appdata, "language_projects", ProjectName)
	}
	if home := userHomeDir(); home != "" {
		return filepath.Join(home, ".language_projects", ProjectName)
	}
	return ""
}

// AddressFile 返回 daemon 地址文件路径。
func AddressFile() string { return filepath.Join(Dir(), AddressFileName) }

// LogFile 返回 daemon 日志文件路径。
func LogFile() string { return filepath.Join(Dir(), LogFileName) }

// userHomeDir 依次尝试 USERPROFILE / HOME 环境变量获取用户主目录。
func userHomeDir() string {
	home := os.Getenv("USERPROFILE")
	if home == "" {
		home = os.Getenv("HOME")
	}
	return home
}
