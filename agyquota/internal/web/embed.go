package web

import "embed"

// Assets 是内嵌 WebUI 的静态资源（HTML / CSS / JS），
// 由 internal/app 的本地 HTTP 服务直接提供，不依赖任何外部网络资源。
//
//go:embed assets/*
var Assets embed.FS
