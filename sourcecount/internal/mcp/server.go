// Package mcp 把 sourcecount 公共 Server 层适配为 MCP stdio server。
package mcp

import (
	"context"
	"errors"
	"log"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"sourcecount/internal/service"
)

// Version 是 MCP server 的版本注入点。
var Version = "dev"

// NewServer 创建注册了全部工具的 MCP server。业务对象无持久化状态。
func NewServer() *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "sourcecount", Version: Version}, nil)
	registerTools(server, service.New())
	return server
}

// Run 启动 stdio MCP server，stdout 只允许协议字节。
func Run(ctx context.Context) error {
	log.SetPrefix("[sourcecount-mcp] ")
	log.SetFlags(log.LstdFlags)
	return NewServer().Run(ctx, &mcp.StdioTransport{})
}

func errResult(err error) *mcp.CallToolResult {
	var serviceErr *service.Error
	if !errors.As(err, &serviceErr) {
		serviceErr = &service.Error{Code: "internal", Message: err.Error()}
	}
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: serviceErr.Code + ": " + serviceErr.Message}},
	}
}
