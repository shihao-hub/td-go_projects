// Package web 内嵌 termhub 的前端静态资源（原生 TS 编译产物 + xterm.js）。
package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed static
var staticFS embed.FS

// Handler 返回静态文件处理器。
func Handler() http.Handler {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err)
	}
	return http.FileServerFS(sub)
}
