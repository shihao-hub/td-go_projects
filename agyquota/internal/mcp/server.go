// Package mcp 实现 agyquota 的 stdio MCP server：把 service 层暴露为 MCP 工具
// （工具发现/参数校验/结构化结果由 SDK 处理）。
// 协议 stdout 只走 SDK 通道，业务日志全部 stderr，绝不混流。
package mcp

import (
	"context"
	"log"
	"os"

	"agyquota/internal/service"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// init 在首次 schema 推导前设置 jsonschema-go（go-sdk 的推导引擎）的
// 兼容开关：切片只推导 "type":"array"，不产生 ["null","array"] 并集
// （数组形式 type 会被部分 MCP 客户端拒绝）。该开关为库提供的兼容
// 机制，已设置时尊重用户环境不覆盖。
func init() {
	if os.Getenv("JSONSCHEMAGODEBUG") == "" {
		os.Setenv("JSONSCHEMAGODEBUG", "typeschemasnull=1")
	}
}

// Version 版本号，由 cli 包注入（与 --version 同源）。
var Version = "dev"

// NewServer 构造注册了全部工具的 MCP server。
// 工具定义与 schema 导出（schema 子命令）同源于 registerTools。
func NewServer() *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "agyquota", Version: Version}, nil)
	registerTools(s)
	return s
}

// Run 启动 stdio MCP server，阻塞至客户端断开。
// log 默认输出到 stderr，前缀标记来源，确保协议 stdout 纯净。
func Run(ctx context.Context) error {
	log.SetPrefix("[agyquota-mcp] ")
	log.SetFlags(log.LstdFlags)
	return NewServer().Run(ctx, &mcp.StdioTransport{})
}

// mustSvc 创建业务服务（无状态、零落盘，无惰性初始化需求）。
func mustSvc() *service.Service {
	return service.New()
}
