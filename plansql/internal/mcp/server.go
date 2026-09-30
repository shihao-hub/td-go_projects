package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"plansql/internal/cli"

	mcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// ListInput list 参数定义
type ListInput struct{}

// SetStatusInput set_status 参数定义
type SetStatusInput struct {
	Path   string `json:"path" jsonschema:"description=Plan or Spec file path relative to workspace,required"`
	Status string `json:"status" jsonschema:"description=Status shorthand (completed/in_progress/pending/abandoned) or JSON string,required"`
	Type   string `json:"type,omitempty" jsonschema:"description=Document type (plan or spec, optional)"`
}

// CheckInput check 参数定义
type CheckInput struct{}

// ScanInput scan 参数定义
type ScanInput struct{}

// RunMCPServer 启动 stdio MCP 协议服务器
func RunMCPServer(host string) error {
	client, err := cli.NewClient(host)
	if err != nil {
		return fmt.Errorf("create client: %w", err)
	}

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "plansql",
		Version: "1.0.0",
	}, nil)

	// 工具 1: plansql_list
	mcp.AddTool(server, &mcp.Tool{
		Name:        "plansql_list",
		Description: "List all tracked plan and spec status entries from the SQLite projection.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input *ListInput) (*mcp.CallToolResult, any, error) {
		items, err := client.GetItems()
		if err != nil {
			return nil, nil, err
		}
		data, _ := json.Marshal(items)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: string(data)},
			},
		}, nil, nil
	})

	// 工具 2: plansql_set_status
	mcp.AddTool(server, &mcp.Tool{
		Name:        "plansql_set_status",
		Description: "Update a plan/spec status by safely appending an atomic SQL mutation to the append-only SQL log.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input *SetStatusInput) (*mcp.CallToolResult, any, error) {
		itemType := input.Type
		if itemType == "" {
			itemType = "plan"
		}

		statusVal := input.Status
		if statusVal == "completed" || statusVal == "in_progress" || statusVal == "pending" || statusVal == "abandoned" {
			statusVal = fmt.Sprintf(`{"state":"%s"}`, statusVal)
		}

		sqlGenerated, err := client.AppendMutation(itemType, input.Path, statusVal)
		if err != nil {
			return nil, nil, err
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Successfully appended SQL mutation:\n%s", sqlGenerated)},
			},
		}, nil, nil
	})

	// 工具 3: plansql_check
	mcp.AddTool(server, &mcp.Tool{
		Name:        "plansql_check",
		Description: "Verify the syntax and integrity of the append-only SQL file.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input *CheckInput) (*mcp.CallToolResult, any, error) {
		res, err := client.CheckSQL()
		if err != nil {
			return nil, nil, err
		}
		data, _ := json.Marshal(res)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: string(data)},
			},
		}, nil, nil
	})

	// 工具 4: plansql_scan
	mcp.AddTool(server, &mcp.Tool{
		Name:        "plansql_scan",
		Description: "Scan the workspace file system for **/plans/** and **/specs/** to reconcile with recorded SQL status.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input *ScanInput) (*mcp.CallToolResult, any, error) {
		report, err := client.ScanDocs()
		if err != nil {
			return nil, nil, err
		}
		data, _ := json.Marshal(report)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: string(data)},
			},
		}, nil, nil
	})

	return server.Run(context.Background(), &mcp.StdioTransport{})
}

// ExportSchema 导出工具元数据和 Schema
func ExportSchema() {
	schemas := map[string]any{
		"tools": []map[string]any{
			{
				"name":        "plansql_list",
				"description": "List all tracked plan and spec status entries.",
			},
			{
				"name":        "plansql_set_status",
				"description": "Update a plan/spec status by appending an atomic SQL mutation.",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"path":   map[string]any{"type": "string", "description": "Relative path to markdown document"},
						"status": map[string]any{"type": "string", "description": "Status name or JSON"},
						"type":   map[string]any{"type": "string", "enum": []string{"plan", "spec"}},
					},
					"required": []string{"path", "status"},
				},
			},
			{
				"name":        "plansql_check",
				"description": "Verify the syntax and integrity of the append-only SQL file.",
			},
			{
				"name":        "plansql_scan",
				"description": "Scan workspace to reconcile plans/specs on disk against SQL database.",
			},
		},
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(schemas)
}
