// Package buildinfo 是构建指纹（Version / BuildID / BuildMode）的单一真理源。
// 构建脚本经 -ldflags -X 注入；未注入时回退到 VCS 嵌入信息，保证开发调试也有稳定指纹。
package buildinfo

import (
	"runtime/debug"
)

var (
	// Version 是展示的版本号（如 v1.0.0 或 v1.0.0-dev）。
	Version = ""
	// BuildID 是构建唯一标识（commit hash + 时间戳）。
	BuildID = ""
	// BuildMode 是通道唯一真源："dev" 或 "release"，由构建脚本注入。
	BuildMode = ""
)

func init() {
	v := vcsVersion()
	if Version == "" {
		Version = v + "-dev"
	}
	if BuildID == "" {
		BuildID = v
	}
	if BuildMode == "" {
		BuildMode = "dev"
	}
}

// IsRelease 报告当前二进制是否为正式发行通道。
func IsRelease() bool {
	return BuildMode == "release"
}

// ChannelName 返回当前通道名称："prod" 或 "dev"。
func ChannelName() string {
	if IsRelease() {
		return "prod"
	}
	return "dev"
}

// vcsVersion 从编译期嵌入的 VCS 信息提取短 commit，取不到返回 "unknown"。
func vcsVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	var revision string
	var modified bool
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			revision = s.Value
		case "vcs.modified":
			modified = s.Value == "true"
		}
	}
	if revision == "" {
		return "unknown"
	}
	short := revision
	if len(short) > 7 {
		short = short[:7]
	}
	if modified {
		return short + "-dirty"
	}
	return short
}
