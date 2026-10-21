package web

import "embed"

// Assets 内嵌 WebUI 静态资产，由 GUI 与 CLI serve 共用的 HTTP Handler 托管。
//
//go:embed assets/*
var Assets embed.FS
