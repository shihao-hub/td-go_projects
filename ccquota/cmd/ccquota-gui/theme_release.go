//go:build !dev

package main

import "github.com/wailsapp/wails/v2/pkg/options/windows"

// 正式版固定深色标题栏，与专属透明图标和 WebUI 配色一致。
const windowTheme = windows.Dark
