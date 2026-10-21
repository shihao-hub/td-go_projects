package web

import "embed"

// Assets 内嵌 WebUI 静态资产，由 internal/api 在根路径托管。
//
//go:embed assets/*
var Assets embed.FS
