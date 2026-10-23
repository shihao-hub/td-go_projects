// Package buildinfo 承载编译期注入的版本与构建通道。
//
// 它不依赖 Wails 或任何 GUI 代码，因此 console 通道产物引用它时
// 不会被拖入桌面壳依赖。
package buildinfo

var (
	// Version 编译期通过 -ldflags "-X taskbarguard/internal/buildinfo.Version=..." 注入。
	Version = "dev"
	// BuildMode 编译期通过 -ldflags "-X taskbarguard/internal/buildinfo.BuildMode=release" 注入。
	BuildMode = "dev"
	// ScriptsHash 编译期注入的 scripts 资产指纹；为空时运行期扫描内嵌文件树计算。
	ScriptsHash = ""
)

// IsRelease 返回当前二进制是否为正式发行版；单一真源是 BuildMode。
func IsRelease() bool {
	return BuildMode == "release"
}

// Channel 返回构建通道：release → prod，dev → dev。
func Channel() string {
	if IsRelease() {
		return "prod"
	}
	return "dev"
}
