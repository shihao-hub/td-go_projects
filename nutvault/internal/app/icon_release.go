//go:build !dev

package app

import _ "embed"

// iconICO 正式版图标
//
//go:embed icon.ico
var iconICO []byte
