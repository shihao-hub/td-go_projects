// Package mcp 实现 clictl mcp 子命令：标准 MCP stdio server，
// 把 service 层暴露为 MCP 工具（工具发现/参数校验/结构化结果全部由 SDK 处理）。
// 协议 stdout 只走 SDK 通道，业务日志全部 stderr，绝不混流。
package mcp

import (
	"context"
	"errors"
	"log"
	"sync"

	"clictl/internal/service"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Version 版本号，由 cli 包注入（与构建命令 -ldflags -X 注入同源）
var Version = "dev"

var (
	svcOnce sync.Once
	svcInst *service.Service
	svcErr  error
)

// mustSvc 惰性打开业务服务：首个工具调用时才打开数据库，
// 工具列表/schema 导出等无业务调用时不触发
func mustSvc() (*service.Service, error) {
	svcOnce.Do(func() {
		svcInst, svcErr = service.Open()
	})
	return svcInst, svcErr
}

// NewServer 构造注册了全部工具的 MCP server。
// 工具定义与 schema 导出（clictl schema 子命令）同源于 registerTools。
func NewServer() *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "clictl", Version: Version}, nil)
	registerTools(s)
	return s
}

// Run 启动 stdio MCP server，阻塞至客户端断开。
// log 默认输出到 stderr，前缀标记来源，确保协议 stdout 纯净。
func Run(ctx context.Context) error {
	log.SetPrefix("[clictl-mcp] ")
	log.SetFlags(log.LstdFlags)
	return NewServer().Run(ctx, &mcp.StdioTransport{})
}

// errResult 把业务错误转为 IsError 结果。MCP 规范：工具自身的业务错误
// 走结果（IsError + 内容）而非协议级错误，客户端才能看到并自我纠正。
// 文本格式 "code: message"，与 CLI errText 展示一致。
func errResult(err error) *mcp.CallToolResult {
	var se *service.Error
	if !errors.As(err, &se) {
		se = &service.Error{Code: "internal", Message: err.Error()}
	}
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: se.Code + ": " + se.Message}},
	}
}
