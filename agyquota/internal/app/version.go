package app

import "agyquota/internal/buildinfo"

// Version / BuildMode 是 GUI 壳展示与门禁判定的便捷入口。
// 真源仍是 internal/buildinfo（--version、daemon buildID 握手共用同一份注入值）。
var (
	Version   = buildinfo.Version
	BuildMode = buildinfo.BuildMode
)

// IsRelease 报告当前二进制是否为正式发行版。
func IsRelease() bool { return buildinfo.IsRelease() }
