//go:build !dev

package main

import _ "embed"

// 正式版窗口图标：由 scripts/render_icon.py 生成的专属透明底图标。
//
//go:embed icon.ico
var windowIcon []byte

// 托盘图标始终用专属透明图标：托盘背景（任务栏）通常是深色，
// 白底小图标会明显难看，与 dev/release 无关。
//
//go:embed icon.ico
var trayIcon []byte
