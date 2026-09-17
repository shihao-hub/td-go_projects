package server

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"
)

// 长轮询超时参数
const (
	defaultWatchTimeout = 30 * time.Second
	maxWatchTimeout     = 120 * time.Second
	// maxBodySize 限制写入请求体大小，防滥用
	maxBodySize = 4 << 20
)

// NewMux 组装全部 HTTP API 路由
func NewMux(st *Store, bc *Broadcaster) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/discovery", func(w http.ResponseWriter, r *http.Request) {
		handleDiscovery(w, st)
	})
	mux.HandleFunc("GET /api/{app}/{env}", func(w http.ResponseWriter, r *http.Request) {
		handleGet(w, r, st)
	})
	mux.HandleFunc("PUT /api/{app}/{env}", func(w http.ResponseWriter, r *http.Request) {
		handlePut(w, r, st, bc)
	})
	mux.HandleFunc("GET /api/watch/{app}/{env}", func(w http.ResponseWriter, r *http.Request) {
		handleWatch(w, r, st, bc)
	})
	return mux
}

// handleGet GET /api/{app}/{env}：返回配置内容与版本元数据
func handleGet(w http.ResponseWriter, r *http.Request, st *Store) {
	app := r.PathValue("app")
	env := r.PathValue("env")
	if !validName(app) || !validName(env) {
		WriteErr(w, http.StatusBadRequest, CodeInvalidName, "app/env must match [a-zA-Z0-9_-]+")
		return
	}
	raw, version, ok := st.Get(key{App: app, Env: env})
	if !ok {
		WriteErr(w, http.StatusNotFound, CodeNotFound, "config not found: "+app+"/"+env)
		return
	}
	WriteJSON(w, http.StatusOK, CodeOK, "", map[string]any{
		"content": raw,
		"version": version,
	})
}

// handlePut PUT /api/{app}/{env}：body 即配置 JSON 对象，成功返回新版本
func handlePut(w http.ResponseWriter, r *http.Request, st *Store, bc *Broadcaster) {
	app := r.PathValue("app")
	env := r.PathValue("env")
	if !validName(app) || !validName(env) {
		WriteErr(w, http.StatusBadRequest, CodeInvalidName, "app/env must match [a-zA-Z0-9_-]+")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodySize+1))
	if err != nil {
		WriteErr(w, http.StatusBadRequest, CodeInvalidJSON, "read body failed")
		return
	}
	if len(body) > maxBodySize {
		WriteErr(w, http.StatusRequestEntityTooLarge, CodeInvalidJSON, "body too large")
		return
	}
	version, err := st.Put(app, env, body)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidName):
			WriteErr(w, http.StatusBadRequest, CodeInvalidName, "app/env must match [a-zA-Z0-9_-]+")
		case errors.Is(err, ErrInvalidJSON):
			WriteErr(w, http.StatusBadRequest, CodeInvalidJSON, "body must be a json object")
		default:
			slog.Error("put config failed", "app", app, "env", env, "err", err)
			WriteErr(w, http.StatusInternalServerError, CodeInternal, "write failed")
		}
		return
	}
	slog.Info("config updated", "app", app, "env", env, "version", version)
	WriteJSON(w, http.StatusOK, CodeOK, "", map[string]any{"version": version})
}

// discoveryApp 发现接口的单应用条目
type discoveryApp struct {
	App  string         `json:"app"`
	Envs []discoveryEnv `json:"envs"`
}

// discoveryEnv 发现接口的单环境条目
type discoveryEnv struct {
	Env     string `json:"env"`
	Version uint64 `json:"version"`
}

// handleDiscovery GET /api/discovery：列出全部 app/env/version
func handleDiscovery(w http.ResponseWriter, st *Store) {
	entries := st.List()
	appOrder := make([]string, 0, len(entries))
	envsByApp := make(map[string][]discoveryEnv)
	for _, e := range entries {
		if _, ok := envsByApp[e.App]; !ok {
			appOrder = append(appOrder, e.App)
		}
		envsByApp[e.App] = append(envsByApp[e.App], discoveryEnv{Env: e.Env, Version: e.Version})
	}
	apps := make([]discoveryApp, 0, len(appOrder))
	for _, app := range appOrder {
		apps = append(apps, discoveryApp{App: app, Envs: envsByApp[app]})
	}
	WriteJSON(w, http.StatusOK, CodeOK, "", map[string]any{"apps": apps})
}

// handleWatch GET /api/watch/{app}/{env}?version=N：长轮询变更通知。
// 当前版本 != N 立即返回最新；相等则挂起，超时/唤醒后返回与读取接口相同的响应。
func handleWatch(w http.ResponseWriter, r *http.Request, st *Store, bc *Broadcaster) {
	app := r.PathValue("app")
	env := r.PathValue("env")
	if !validName(app) || !validName(env) {
		WriteErr(w, http.StatusBadRequest, CodeInvalidName, "app/env must match [a-zA-Z0-9_-]+")
		return
	}
	known := parseVersionParam(r.URL.Query().Get("version"))
	timeout := parseTimeoutParam(r.URL.Query().Get("timeout"))

	k := key{App: app, Env: env}
	_, cur, ok := st.Get(k)
	if ok && cur != known {
		// 版本已更新（含首次订阅 N=0）：立即返回，不挂起
		writeWatchState(w, st, k)
		return
	}
	if !ok {
		WriteErr(w, http.StatusNotFound, CodeNotFound, "config not found: "+app+"/"+env)
		return
	}
	bc.Wait(r, k, timeout)
	writeWatchState(w, st, k)
}

// writeWatchState 返回与读取接口相同的响应包络
func writeWatchState(w http.ResponseWriter, st *Store, k key) {
	raw, version, ok := st.Get(k)
	if !ok {
		WriteErr(w, http.StatusNotFound, CodeNotFound, "config not found: "+k.App+"/"+k.Env)
		return
	}
	WriteJSON(w, http.StatusOK, CodeOK, "", map[string]any{
		"content": raw,
		"version": version,
	})
}

// parseVersionParam 解析 version 参数，失败按 0 处理
func parseVersionParam(s string) uint64 {
	if s == "" {
		return 0
	}
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0
	}
	return n
}

// parseTimeoutParam 解析 timeout 参数（单位秒），默认 30s，上限 120s
func parseTimeoutParam(s string) time.Duration {
	if s == "" {
		return defaultWatchTimeout
	}
	sec, err := strconv.ParseInt(s, 10, 64)
	if err != nil || sec <= 0 {
		return defaultWatchTimeout
	}
	d := time.Duration(sec) * time.Second
	if d > maxWatchTimeout {
		return maxWatchTimeout
	}
	return d
}
