// Package version 承载构建期经 -ldflags 注入的版本信息。
package version

// 构建变量（build.py 经 -ldflags "-X typeai-gui/version.Version=..." 注入）。
var (
	Version = "dev"
	Commit  = "unknown"
)
