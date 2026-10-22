package app

import "typeai/internal/buildinfo"

// Version 返回构建注入的版本文本。
func Version() string { return buildinfo.Version }

// BuildID 返回构建指纹。
func BuildID() string { return buildinfo.BuildID }

// BuildMode 返回构建脚本注入的通道标识（release / dev）。
func BuildMode() string { return buildinfo.BuildMode }

// IsRelease 返回当前二进制是否为正式发行版。
func IsRelease() bool { return buildinfo.IsProduction() }

// Channel 返回当前构建通道名：release → prod，dev → dev。
func Channel() string { return buildinfo.Channel() }
