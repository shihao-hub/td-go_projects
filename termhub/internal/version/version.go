// Package version termhub 构建信息（ldflags 注入；T08 完善 dev/prod 判定）。
package version

// Version 版本号，build 时经 -ldflags 注入。
var Version = "v0.0.0-dev"

// Channel 返回构建通道简称（T01 固定 dev；T08 按构建形态翻转）。
func Channel() string { return "dev" }
