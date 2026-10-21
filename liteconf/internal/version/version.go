// Package version 存放构建期注入的版本号与构建通道。
// scripts/build.py 通过 -ldflags "-X github.com/shihao-hub/liteconf/internal/version.Version=<v>"
// 与 ".../BuildMode=<dev|release>" 注入，GUI 与 CLI 两个产物共享同一组真源。
package version

var (
	// Version 为当前版本号，构建未注入时回退 dev。
	Version = "dev"
	// BuildMode 为构建通道（dev / release），是 IsRelease 与 Channel 的唯一真源。
	BuildMode = "dev"
)

// IsRelease 返回当前二进制是否为正式发行版。
// 只认编译期注入的 BuildMode，禁止根据 Version 文本猜测。
func IsRelease() bool {
	return BuildMode == "release"
}

// Channel 返回当前构建通道名：release → prod，dev → dev。
// 数据目录、单实例 ID、日志路径全部由它派生。
func Channel() string {
	if IsRelease() {
		return "prod"
	}
	return "dev"
}
