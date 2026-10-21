package app

import "plansql/internal/buildinfo"

// Version 来自构建注入的 git describe 文本。
func Version() string { return buildinfo.Version }

// BuildID 是构建指纹（describe + 时间戳）。
func BuildID() string { return buildinfo.BuildID }

// BuildMode 是构建脚本注入的通道标识（release / dev）。
func BuildMode() string { return buildinfo.BuildMode }

// IsRelease 返回当前二进制是否为正式发行版。
// 单一真源：只认编译期注入的 BuildMode，不用 Version 文本猜测。
func IsRelease() bool { return buildinfo.IsProduction() }

// Channel 返回当前构建通道名：release → prod，dev → dev。
func Channel() string { return buildinfo.Channel() }
