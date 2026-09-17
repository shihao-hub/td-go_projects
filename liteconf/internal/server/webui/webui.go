// Package webui 提供内嵌 Web 控制台的静态资源托管。
// 只做只读托管与渲染，不包含任何存储、校验、版本管理等业务逻辑（业务一律走 /api/*）。
package webui

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed static
var staticFS embed.FS

// Handler 返回控制台静态资源处理器，供 server 挂载到 GET /ui/。
func Handler() http.Handler {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		// embed 路径在编译期由 //go:embed 保证，运行时不可能失败
		panic(err)
	}
	// FileServerFS 按完整请求路径查文件，挂载在 /ui/ 下需剥离前缀，
	// 否则请求 /ui/app.js 会在子文件系统里找 "ui/app.js" 而命中 404
	return noCache(http.StripPrefix("/ui", http.FileServerFS(sub)))
}

// noCache 中间件：静态页体积小，禁用浏览器缓存，避免发版后持旧页面导致前后端不一致。
func noCache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		next.ServeHTTP(w, r)
	})
}
