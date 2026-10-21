//go:build !cli

package main

// consoleBuild 标明当前产物为 windowsgui 子系统（桌面优先）：
// 双击零黑框，无参数运行时启动桌面工作台与托盘。
const consoleBuild = false
