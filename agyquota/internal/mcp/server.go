// Package mcp 实现 agyquota 的 stdio MCP server：把 daemon HTTP API 暴露为
// MCP 工具（工具发现/参数校验/结构化结果由 SDK 处理）。
// 本包是 daemon 的客户端（经 internal/client），不直接依赖 internal/service；
// 协议 stdout 只走 SDK 通道，业务日志全部 stderr，绝不混流。
package mcp

import (
	"context"
	"log"
	"os"

	"agyquota/internal/buildinfo"
	"agyquota/internal/client"

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

// NewServer 构造注册了全部工具的 MCP server。
// 工具定义与 schema 导出（schema 子命令）同源于 registerTools；
// 启动不触达 daemon，仅工具调用时经 client 执行 EnsureDaemon。
func NewServer(cfg client.Config) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "agyquota", Version: buildinfo.Version}, nil)
	registerTools(s, cfg)
	return s
}

// Run 启动 stdio MCP server，阻塞至客户端断开。
// log 默认输出到 stderr，前缀标记来源，确保协议 stdout 纯净。
func Run(ctx context.Context, cfg client.Config) error {
	log.SetPrefix("[agyquota-mcp] ")
	log.SetFlags(log.LstdFlags)
	return NewServer(cfg).Run(ctx, &mcp.StdioTransport{})
}

// mustClient 创建 daemon HTTP 客户端（无状态，按需发现与拉起）。
func mustClient(cfg client.Config) *client.Client {
	return client.New(cfg)
}
