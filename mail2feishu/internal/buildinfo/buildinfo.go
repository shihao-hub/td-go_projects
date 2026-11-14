// Package buildinfo 保存构建期通过 -ldflags 注入的版本信息。
package buildinfo

var (
	// Version 形如 v0.1.0 或 v0.1.0-dev / v0.1.0-dev-dirty。
	Version = "v0.0.0-dev"
	// Mode 为 dev 或 release。
	Mode = "dev"
	// BuildID 是 git 短哈希，工作区不干净时带 -dirty。
	BuildID = "unknown"
)
