package app

import "github.com/shihao-hub/liteconf/internal/appenv"

// Recover 转发 appenv.Recover，供 main 作为 defer 兜底。
func Recover(withDialog bool) {
	appenv.Recover(withDialog)
}
