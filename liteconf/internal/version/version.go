// Package version 存放构建期注入的版本号。
// scripts/build.ps1 通过 -ldflags "-X github.com/shihao-hub/liteconf/internal/version.Version=<v>"
// 为 liteconf-server 与 liteconf 两个二进制注入同一版本号。
package version

// Version 为当前版本号，构建未注入时回退 dev。
var Version = "dev"
