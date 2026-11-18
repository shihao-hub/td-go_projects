//go:build dev

package main

import _ "embed"

// 开发版窗口图标：沿用仓库默认的 Go 图标（与 typeai / clictl 的 dev 通道一致）。
// 白底图在深色标题栏下会显出一个白色方块——这是默认图标自带的，不是渲染问题。
//
//go:embed icon_dev.ico
var windowIcon []byte

// 托盘图标始终用专属透明图标（见 icon_release.go 的说明）。
//
//go:embed icon.ico
var trayIcon []byte
