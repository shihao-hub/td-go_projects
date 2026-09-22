package mcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Schema 经 in-memory 连接执行真实的 tools/list，导出与协议完全同源的
// 工具定义（含 input/output JSON Schema），供 schema 子命令离线检查与
// 联调对照。不触发业务网络调用。
func Schema(ctx context.Context) ([]*mcp.Tool, error) {
	server := NewServer()
	client := mcp.NewClient(&mcp.Implementation{Name: "agyquota-schema-export"}, nil)

	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		return nil, err
	}
	defer serverSession.Close()

	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		return nil, err
	}
	defer clientSession.Close()

	res, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		return nil, err
	}
	return res.Tools, nil
}
