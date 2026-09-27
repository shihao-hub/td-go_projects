package mcp

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"sourcecount/internal/contract"
	"sourcecount/internal/service"
)

type scanInput struct {
	Roots             []string `json:"roots,omitempty" jsonschema:"扫描根路径；缺省为 server 当前工作目录"`
	ConfigPath        string   `json:"config_path,omitempty" jsonschema:"显式 JSON 配置路径"`
	Include           []string `json:"include,omitempty" jsonschema:"覆盖配置 include 的 Glob 列表"`
	Exclude           []string `json:"exclude,omitempty" jsonschema:"覆盖配置 exclude 的 Glob 列表"`
	TextExtensions    []string `json:"text_extensions,omitempty" jsonschema:"强制文本后缀列表"`
	BinaryExtensions  []string `json:"binary_extensions,omitempty" jsonschema:"强制二进制后缀列表"`
	NoDefaultExcludes bool     `json:"no_default_excludes,omitempty" jsonschema:"关闭内置默认排除目录"`
}

func registerTools(server *mcp.Server, svc *service.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "sourcecount.project.scan",
		Description: "按文件后缀统计项目中的文本和二进制文件，支持配置过滤与后缀类型覆盖。",
		Annotations: &mcp.ToolAnnotations{Title: "统计项目源码", ReadOnlyHint: true, OpenWorldHint: boolPtr(false)},
	}, func(ctx context.Context, req *mcp.CallToolRequest, in scanInput) (*mcp.CallToolResult, contract.ScanReport, error) {
		_ = req
		out, err := svc.Scan(ctx, contract.ScanRequest{
			Roots:             in.Roots,
			ConfigPath:        in.ConfigPath,
			Include:           in.Include,
			Exclude:           in.Exclude,
			TextExtensions:    in.TextExtensions,
			BinaryExtensions:  in.BinaryExtensions,
			NoDefaultExcludes: in.NoDefaultExcludes,
		})
		if err != nil {
			return errResult(err), contract.ScanReport{}, nil
		}
		if len(out.Errors) > 0 {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("partial_scan: 扫描完成，但有 %d 个文件未能统计", len(out.Errors))}},
			}, out, nil
		}
		return nil, out, nil
	})
}

func boolPtr(value bool) *bool { return &value }
