//go:build dev

package main

import "github.com/wailsapp/wails/v2/pkg/options/windows"

// 开发版跟随系统主题：默认图标是白底的，深色标题栏下会露出白块。
const windowTheme = windows.SystemDefault
