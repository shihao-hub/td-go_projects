package app

var (
	// Version 编译期通过 -ldflags "-X deepseeksearch/internal/app.Version=..." 注入
	Version = "dev"
	// BuildMode 编译期通过 -ldflags "-X deepseeksearch/internal/app.BuildMode=release" 注入
	BuildMode = "dev"
)

// IsRelease 返回当前二进制是否为正式发行版
func IsRelease() bool {
	return BuildMode == "release"
}

// Channel 返回当前构建通道名：release → prod，dev → dev
func Channel() string {
	if IsRelease() {
		return "prod"
	}
	return "dev"
}
