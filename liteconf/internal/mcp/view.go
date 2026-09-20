package mcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ToolSpecs 经 in-memory transport 读取实际注册的工具视图
// （《CLI 工具开发标准》5.5 推荐的同源路径：直接导出注册定义，
// schema 子命令的 tools 由此派生，不维护另一份手抄目录）。
//
// 只读注册元数据，不触碰网络与业务依赖（标准 4.3）；
// 完整遍历 tools/list 全部分页（标准 9.1：一次调用不能证明完整发现）。
func ToolSpecs(ctx context.Context, cfg Config) ([]*mcp.Tool, error) {
	srv, err := newServer(cfg)
	if err != nil {
		return nil, err
	}
	st, ct := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, st, nil)
	if err != nil {
		return nil, err
	}
	defer ss.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: serverName + "-schema", Version: cfg.Version}, nil)
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		return nil, err
	}
	defer cs.Close()

	var tools []*mcp.Tool
	params := &mcp.ListToolsParams{}
	for {
		res, err := cs.ListTools(ctx, params)
		if err != nil {
			return nil, err
		}
		tools = append(tools, res.Tools...)
		if res.NextCursor == "" {
			break
		}
		params.Cursor = res.NextCursor
	}
	return tools, nil
}
