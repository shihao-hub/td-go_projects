//go:build dev

package app

import _ "embed"

// iconICO 开发版强制使用 Go 地鼠图标，便于一眼区分 dev / release 产物。
//
//go:embed icon_dev.ico
var iconICO []byte
