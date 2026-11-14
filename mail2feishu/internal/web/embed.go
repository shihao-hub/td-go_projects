// Package web 内嵌 WebUI 静态资产，由本地 HTTP 服务托管。
package web

import "embed"

// Assets 是内嵌的静态资源。
//
//go:embed assets/*
var Assets embed.FS
