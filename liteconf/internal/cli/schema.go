package cli

import (
	"context"
	"encoding/json"
	"fmt"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/shihao-hub/liteconf/internal/mcp"
	"github.com/shihao-hub/liteconf/internal/version"
)

// contract 为 schema 子命令导出的顶层契约目录：
// tools 与 MCP 实际注册同源（internal/mcp.ToolSpecs，in-memory 读注册视图），
// commands 为 CLI 命令契约（catalog.go 同源）。
// 自提供 MCP 入口起不再适用 interface:"cli" 例外
// （《CLI 工具开发标准》5.5），存量契约变更已在 README 记录。
type contract struct {
	Name     string         `json:"name"`
	Version  string         `json:"version"`
	Tools    []*sdkmcp.Tool `json:"tools"`
	Commands []command      `json:"commands"`
}

// runSchema 输出契约目录（JSON）：
// 只读注册元数据，不发起网络请求、不启动任何外部程序。
func runSchema(opt *Options) int {
	tools, err := mcp.ToolSpecs(context.Background(), mcp.Config{
		ServerURL: mcp.DefaultServerURL,
		Version:   version.Version,
	})
	if err != nil {
		fmt.Fprintf(opt.Stderr, "liteconf schema: 读取 MCP 注册视图失败：%v\n", err)
		return 1
	}
	c := contract{
		Name:     "liteconf",
		Version:  version.Version,
		Tools:    tools,
		Commands: commands,
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		fmt.Fprintf(opt.Stderr, "liteconf schema: 序列化契约失败：%v\n", err)
		return 1
	}
	if _, err := opt.Stdout.Write(append(data, '\n')); err != nil {
		fmt.Fprintf(opt.Stderr, "liteconf schema: 写出失败：%v\n", err)
		return 1
	}
	return 0
}
