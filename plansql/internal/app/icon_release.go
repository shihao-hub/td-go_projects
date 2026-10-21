//go:build !dev

package app

import _ "embed"

// iconICO 正式版专属图标，由 scripts/render_icon.py 生成并入库。
//
//go:embed icon.ico
var iconICO []byte
