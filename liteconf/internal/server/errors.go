// Package server 实现 liteconf 配置中心服务端：
// 配置存储、读写/发现/长轮询 API 与外部编辑检测。
package server

import (
	"encoding/json"
	"net/http"
	"regexp"
)

// 业务错误 code：写入端与读取端共用，保持稳定
const (
	CodeOK           = "ok"
	CodeNotFound     = "not_found"
	CodeInvalidName  = "invalid_name"
	CodeInvalidJSON  = "invalid_json"
	CodeInternal     = "internal"
	CodeInvalidWatch = "invalid_watch"
)

// namePattern 限定 app/env 名称，防目录穿越
var namePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// validName 校验 app/env 名称合法性
func validName(s string) bool {
	return s != "" && len(s) <= 128 && namePattern.MatchString(s)
}

// Response 统一响应包络
type Response struct {
	Code    string `json:"code"`
	Message string `json:"message,omitempty"`
	Data    any    `json:"data,omitempty"`
}

// WriteJSON 以统一包络写出 JSON 响应
func WriteJSON(w http.ResponseWriter, status int, code, msg string, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(Response{Code: code, Message: msg, Data: data})
}

// WriteErr 以统一包络写出错误响应（data 为空）
func WriteErr(w http.ResponseWriter, status int, code, msg string) {
	WriteJSON(w, status, code, msg, nil)
}
