package mcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// DefaultServerURL 默认连接的 liteconf server 地址（与 liteconf-server 默认监听一致）
const DefaultServerURL = "http://127.0.0.1:8646"

// serverName MCP server 名与工具命名前缀
// （《CLI 工具开发标准》5.2 三段式 <prog>.<资源>.<动词>；
// http 为纯终端透传、无资源段，故工具名直接映射 API 语义）
const serverName = "liteconf"

// 工具名常量：与 README MCP 工具表、schema 导出同源
const (
	toolDiscovery = serverName + ".discovery"
	toolGetConfig = serverName + ".config.get"
	toolPutConfig = serverName + ".config.put"
)

// Config MCP server 启动配置；与单次调用的业务参数分离（标准 4.3）
type Config struct {
	// ServerURL 运行中 liteconf server 地址，如 http://127.0.0.1:8646
	ServerURL string
	// Version 构建期注入的版本号（serverInfo.version）
	Version string
}

// newServer 构建 MCP server 并注册全部工具；transport 无关，
// 供 Run 与注册视图导出（view.go）共用。
func newServer(cfg Config) (*mcp.Server, error) {
	s := &mcpServer{api: newAPIClient(cfg.ServerURL)}
	srv := mcp.NewServer(&mcp.Implementation{Name: serverName, Version: cfg.Version}, nil)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        toolDiscovery,
		Description: "列出 liteconf 配置中心全部应用/环境/当前版本。只读操作。",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, s.callDiscovery)

	mcp.AddTool(srv, &mcp.Tool{
		Name: toolGetConfig,
		Description: "读取一份配置（app/env 必填），返回 content（配置 JSON 对象）与 version。" +
			"可选 path 为点路径（如 db.host），只取子值，未命中返回 path_not_found。只读操作。",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, s.callGet)

	mcp.AddTool(srv, &mcp.Tool{
		Name: toolPutConfig,
		Description: "整体覆盖写入一份配置：content 必须是完整 JSON 对象，server 原子写并广播热更新，" +
			"成功返回新 version（每次 +1）。破坏性：直接覆盖现有配置，无确认、无历史版本。" +
			"app/env 仅允许 [a-zA-Z0-9_-]（最长 128），单次写入上限 4MiB。",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:    false,
			DestructiveHint: boolPtr(true),
			IdempotentHint:  false,
		},
	}, s.callPut)

	return srv, nil
}

// boolPtr 便捷指针（DestructiveHint / OpenWorldHint 为 *bool）
func boolPtr(b bool) *bool { return &b }

// Run 在给定 transport 上服务直到连接关闭。
// 生产路径传 &mcp.StdioTransport{}：stdin/stdout 专用于协议，
// 启动诊断与日志由调用方写 stderr。
func Run(ctx context.Context, cfg Config, t mcp.Transport) error {
	srv, err := newServer(cfg)
	if err != nil {
		return err
	}
	return srv.Run(ctx, t)
}
