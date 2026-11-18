// Package server 提供 WebUI 的 HTTP 数据面（只读）。
package server

import (
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"

	"ccquota/internal/appenv"
	"ccquota/internal/service"
)

//go:embed web/*
var webFS embed.FS

// Handler 构造 WebUI 与 REST 接口。
func Handler(svc *service.Service) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/cards", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "只支持 GET", http.StatusMethodNotAllowed)
			return
		}
		opts := service.Options{
			Refresh: r.URL.Query().Get("refresh") == "1",
			App:     r.URL.Query().Get("app"),
		}
		res, err := svc.Cards(r.Context(), opts)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		if err != nil {
			appenv.Logf("api/cards 失败: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":    false,
				"error": map[string]string{"code": "query_failed", "message": err.Error()},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "data": res})
	})

	sub, err := fs.Sub(webFS, "web")
	if err != nil {
		panic(err)
	}
	mux.Handle("/", http.FileServer(http.FS(sub)))
	return mux
}
