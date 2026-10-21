// Package buildinfo 是构建指纹（Version / BuildID / BuildMode）的单点来源。
// 构建脚本经 -ldflags -X 注入；未注入时回退到 Go 编译期嵌入的 VCS 信息，
// 保证 `go run` / 测试构建也能得到稳定的开发指纹。
package buildinfo

import (
	"runtime/debug"
)

var (
	// Version 是 --version 展示的 git describe 结果，由构建脚本注入。
	Version = ""
	// BuildID 是握手与升级检测凭据（describe [+ .时间戳]），由构建脚本注入。
	BuildID = ""
	// BuildMode 是通道唯一真源："dev" 或 "release"，由构建脚本注入。
	// 禁止根据 Version 文本猜测通道，否则会造出「名义 dev、实际 release」的混合体。
	BuildMode = ""
)

func init() {
	v := vcsVersion()
	if Version == "" {
		Version = v
	}
	if BuildID == "" {
		BuildID = v
	}
	if BuildMode == "" {
		BuildMode = "dev"
	}
}

// IsRelease 报告当前二进制是否为正式发行通道。
func IsRelease() bool { return BuildMode == "release" }

// IsProduction 是 IsRelease 的兼容别名：daemon 客户端据此决定是否自动拉起。
func IsProduction() bool { return IsRelease() }

// vcsVersion 从编译期嵌入的 VCS 信息生成 "短hash[-dirty]"，取不到返回 "dev"。
func vcsVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
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
		return "dev"
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
