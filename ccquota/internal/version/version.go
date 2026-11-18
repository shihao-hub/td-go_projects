// Package version 承载构建时注入的版本与构建通道。
// scripts/build.py 通过 -ldflags "-X ccquota/internal/version.Version=<v>"
// 与 "-X ccquota/internal/version.BuildMode=<dev|release>" 注入。
package version

var (
	// Version 为当前版本号；未经注入时为 dev。
	Version = "dev"
	// BuildMode 为构建通道（dev / release），是 IsRelease 与 Channel 的唯一来源。
	BuildMode = "dev"
)

// IsRelease 返回当前二进制是否为正式发布版。
func IsRelease() bool {
	return BuildMode == "release"
}

// Channel 返回当前构建通道对应的数据目录名（release → prod，dev → dev）。
func Channel() string {
	if IsRelease() {
		return "prod"
	}
	return "dev"
}
