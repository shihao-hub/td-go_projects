package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed static/*
var staticFS embed.FS

// Handler 返回静态资源 HTTP Handler
func Handler() http.Handler {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err)
	}
	return http.FileServer(http.FS(sub))
}
