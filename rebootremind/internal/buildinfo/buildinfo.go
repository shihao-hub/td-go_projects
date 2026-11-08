// Package buildinfo 保存构建期注入的版本与通道信息。
package buildinfo

var (
	// Version 由构建脚本通过 -ldflags 注入，形如 v0.1.0 或 v0.1.0-dev。
	Version = "dev"
	// Mode 由构建脚本注入，取值为 dev 或 release。
	Mode = "dev"
	// BuildID 由构建脚本注入，用于诊断同一次构建。
	BuildID = ""
)

// IsRelease 报告当前是否为正式构建。
func IsRelease() bool {
	return Mode == "release"
}

// Channel 返回数据目录使用的形态名：dev 或 prod。
func Channel() string {
	if IsRelease() {
		return "prod"
	}
	return "dev"
}

// DisplayVersion 返回用于 --version 展示的版本串。
func DisplayVersion() string {
	if BuildID == "" {
		return Version
	}
	return Version + " (" + BuildID + ")"
}
