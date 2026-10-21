package app

var (
	// Version 编译期通过 -ldflags "-X anatomymanager/internal/app.Version=..." 注入
	Version = "dev"
	// BuildMode 编译期通过 -ldflags "-X anatomymanager/internal/app.BuildMode=release" 注入
	BuildMode = "dev"
)

// IsRelease 返回当前二进制是否为正式发行版。
// 单一真源：只认编译期注入的 BuildMode，不再根据 Version 文本猜测，
// 避免 --version vX.Y.Z 之类参数造出「名义 dev、实际 release」的混合体。
func IsRelease() bool {
	return BuildMode == "release"
}

// Channel 返回当前构建通道名：release → prod，dev → dev。
// 数据目录、库路径、GUI 读取目标全部由它派生，是「我在哪个库」的唯一答案。
func Channel() string {
	if IsRelease() {
		return "prod"
	}
	return "dev"
}
