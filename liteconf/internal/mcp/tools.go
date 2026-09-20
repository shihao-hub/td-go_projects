package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// emptyIn 无参数工具的输入类型
type emptyIn struct{}

// toolOutput MCP 工具统一结构化输出（对象根）：复用 CLI JSON 包络语义，
// ok 与 isError 按同一完成状态映射（《CLI 工具开发标准》5.2）。
// 成功时 Data 有值，失败时 Error 有值。
type toolOutput[D any] struct {
	OK    bool      `json:"ok"`
	Data  D         `json:"data,omitempty"`
	Error *apiError `json:"error,omitempty"`
}

// configGetData liteconf.config.get 的 data：
// Content 整份读取时为配置对象，带 path 时为点路径下钻的子值
type configGetData struct {
	App     string `json:"app"`
	Env     string `json:"env"`
	Version uint64 `json:"version"`
	Content any    `json:"content"`
}

// configPutData liteconf.config.put 的 data：server 端生成的新版本号
type configPutData struct {
	App     string `json:"app"`
	Env     string `json:"env"`
	Version uint64 `json:"version"`
}

// configGetIn liteconf.config.get 输入；无 omitempty 的字段为必填
// （jsonschema-go 据此生成 inputSchema，非法输入在进 handler 前被 SDK 拒绝）
type configGetIn struct {
	App  string `json:"app" jsonschema:"应用名，仅 [a-zA-Z0-9_-]，最长 128"`
	Env  string `json:"env" jsonschema:"环境名，仅 [a-zA-Z0-9_-]，最长 128"`
	Path string `json:"path,omitempty" jsonschema:"可选点路径（如 db.host）只取子值；空 = 整份配置"`
}

// configPutIn liteconf.config.put 输入
type configPutIn struct {
	App     string         `json:"app" jsonschema:"应用名，仅 [a-zA-Z0-9_-]，最长 128"`
	Env     string         `json:"env" jsonschema:"环境名，仅 [a-zA-Z0-9_-]，最长 128"`
	Content map[string]any `json:"content" jsonschema:"完整配置 JSON 对象，整体覆盖写（单次上限 4MiB）"`
}

// mcpServer 工具实现：仅持有 API 客户端，无自有状态与记账，
// 全部业务规则（名称校验、版本、原子写、广播）由 server 维护
type mcpServer struct {
	api *apiClient
}

// okOutput 成功结果：isError=false，SDK 填充 structuredContent
// 并附带 JSON 文本 content（兼容不支持 structuredContent 的客户端）
func okOutput[D any](d D) (*mcp.CallToolResult, toolOutput[D], error) {
	return nil, toolOutput[D]{OK: true, Data: d}, nil
}

// failOutput 业务失败结果：isError=true，结构化错误可被机器读取。
// 不走 handler 的 error 返回值——泛型 handler 返回 error 会丢弃
// 结构化输出（《CLI 工具开发标准》10.2 已核对 go-sdk v1.8.0 行为）。
func failOutput[D any](ae *apiError) (*mcp.CallToolResult, toolOutput[D], error) {
	return &mcp.CallToolResult{IsError: true}, toolOutput[D]{OK: false, Error: ae}, nil
}

// lookup 按点路径逐级下钻，仅接受对象节点；空 path 返回整份。
// 本期仅 MCP 使用，未来 CLI 需要时再下沉共享。
func lookup(content any, path string) (any, bool) {
	if path == "" {
		return content, true
	}
	cur := content
	for _, seg := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = m[seg]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

// callDiscovery 实现 liteconf.discovery：列出全部应用/环境/版本
func (s *mcpServer) callDiscovery(ctx context.Context, _ *mcp.CallToolRequest, _ emptyIn) (*mcp.CallToolResult, toolOutput[discoveryData], error) {
	d, err := s.api.discovery(ctx)
	if err != nil {
		return failOutput[discoveryData](asAPIError(err))
	}
	return okOutput(d)
}

// callGet 实现 liteconf.config.get：读取配置（可选点路径下钻）
func (s *mcpServer) callGet(ctx context.Context, _ *mcp.CallToolRequest, in configGetIn) (*mcp.CallToolResult, toolOutput[configGetData], error) {
	raw, version, err := s.api.get(ctx, in.App, in.Env)
	if err != nil {
		return failOutput[configGetData](asAPIError(err))
	}
	var content any
	if err := json.Unmarshal(raw, &content); err != nil {
		return failOutput[configGetData](&apiError{Code: CodeInternal, Message: "decode config content: " + err.Error()})
	}
	if in.Path != "" {
		sub, ok := lookup(content, in.Path)
		if !ok {
			return failOutput[configGetData](&apiError{
				Code:    CodePathNotFound,
				Message: fmt.Sprintf("path %q not found in %s/%s", in.Path, in.App, in.Env),
			})
		}
		content = sub
	}
	return okOutput(configGetData{App: in.App, Env: in.Env, Version: version, Content: content})
}

// callPut 实现 liteconf.config.put：整体覆盖写入，返回新版本号
func (s *mcpServer) callPut(ctx context.Context, _ *mcp.CallToolRequest, in configPutIn) (*mcp.CallToolResult, toolOutput[configPutData], error) {
	version, err := s.api.put(ctx, in.App, in.Env, in.Content)
	if err != nil {
		return failOutput[configPutData](asAPIError(err))
	}
	return okOutput(configPutData{App: in.App, Env: in.Env, Version: version})
}
