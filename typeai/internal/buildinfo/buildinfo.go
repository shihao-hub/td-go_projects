// Package buildinfo 保存构建期注入的版本、构建指纹与通道标识。
package buildinfo

// 以下变量由 scripts/build.py 通过 -ldflags -X 注入；直接 go build 时保持 dev。
var (
	Version   = "dev"
	BuildID   = ""
	BuildMode = "dev"
)

// IsProduction 返回当前二进制是否为正式发行版。
// 单一真源：只认编译期注入的 BuildMode，不用 Version 文本猜测。
func IsProduction() bool { return BuildMode == "release" }

// Channel 返回当前构建通道目录名：release → prod，dev → dev。
func Channel() string {
	if IsProduction() {
		return "prod"
	}
	return "dev"
}
