// Package mcp 实现 gqw 的 stdio MCP server：把 service 层暴露为 MCP 工具
// （工具发现/参数校验/结构化结果由 SDK 处理）。
// 协议 stdout 只走 SDK 通道，业务日志全部 stderr，绝不混流。
package mcp

import (
	"context"
	"log"
	"os"
	"sync"

	"glmquotawatch/internal/service"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// init 在首次 schema 推导（NewServer→registerTools→AddTool）前设置
// jsonschema-go（go-sdk 的推导引擎，v0.4.3）的兼容开关：切片只推导
// "type":"array"，不产生 ["null","array"] 并集——数组形式 type 会被部分
// MCP 客户端拒绝（Inspector --strict 的 type-union 警告）。该开关为库
// doc.go 明文提供的兼容机制，值按精确字符串匹配（无逗号分隔语法），
// 已设置时尊重用户环境不覆盖。
func init() {
	if os.Getenv("JSONSCHEMAGODEBUG") == "" {
		os.Setenv("JSONSCHEMAGODEBUG", "typeschemasnull=1")
	}
}

// Version 版本号，由 cli 包注入（与 --version 同源）。
var Version = "dev"

var (
	svcOnce sync.Once
	svcInst *service.Service
	svcErr  error
)

// mustSvc 惰性打开业务服务：首个工具调用时才创建（读配置文件），
// 工具列表/schema 导出等无业务调用时不触发。
func mustSvc() (*service.Service, error) {
	svcOnce.Do(func() {
		svcInst, svcErr = service.Open()
	})
	return svcInst, svcErr
}

// NewServer 构造注册了全部工具的 MCP server。
// 工具定义与 schema 导出（schema 子命令）同源于 registerTools。
func NewServer() *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "glmquotawatch", Version: Version}, nil)
	registerTools(s)
	return s
}

// Run 启动 stdio MCP server，阻塞至客户端断开。
// log 默认输出到 stderr，前缀标记来源，确保协议 stdout 纯净。
func Run(ctx context.Context) error {
	log.SetPrefix("[glmquotawatch-mcp] ")
	log.SetFlags(log.LstdFlags)
	return NewServer().Run(ctx, &mcp.StdioTransport{})
}
