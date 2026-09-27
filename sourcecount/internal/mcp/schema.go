package mcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Schema 通过 in-memory transport 读取真实 tools/list，保证导出的定义与
// server 注册结果同源且不触发扫描。
func Schema(ctx context.Context) ([]*mcp.Tool, error) {
	server := NewServer()
	client := mcp.NewClient(&mcp.Implementation{Name: "sourcecount-schema-export"}, nil)

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

	result, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		return nil, err
	}
	return result.Tools, nil
}
