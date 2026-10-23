// Package taskbarguard 承载随二进制分发的 scripts 资产。
//
// scripts 目录位于模块根，只有根包能把它内嵌进二进制；internal 包通过
// 本包读取内嵌文件树，用于单 exe 分发时的首次释放。
package taskbarguard

import "embed"

// Scripts 是 scripts/ 目录的内嵌快照。
//
// 不加 all: 前缀，Go 会自动排除以点或下划线开头的文件与目录，
// 因此 __pycache__、*.pyc 等运行时缓存不会被编进二进制。
//
//go:embed scripts
var Scripts embed.FS
