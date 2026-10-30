package model

import (
	"encoding/json"
)

// CommandSchema 自描述 JSON 契约目录对象
type CommandSchema struct {
	Name        string                   `json:"name"`
	Version     string                   `json:"version"`
	Description string                   `json:"description"`
	Interface   string                   `json:"interface"`
	Commands    map[string]CommandDetail `json:"commands"`
}

// CommandDetail 单个命令契约
type CommandDetail struct {
	Usage        string                 `json:"usage"`
	Description  string                 `json:"description"`
	InputSchema  map[string]interface{} `json:"input_schema,omitempty"`
	OutputSchema map[string]interface{} `json:"output_schema"`
}

// GetStaticSchema 零 I/O 返回完整的离线自描述 Schema
func GetStaticSchema(version string) ([]byte, error) {
	rootSchema := CommandSchema{
		Name:        "agysess",
		Version:     version,
		Description: "Antigravity (agy) CLI 会话管理与 Windows Terminal 联动工具",
		Interface:   "cli",
		Commands: map[string]CommandDetail{
			"list": {
				Usage:       "agysess list [flags]",
				Description: "查询并过滤 agy 会话列表，支持关键词、项目、状态过滤与多维排序",
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"query":    map[string]interface{}{"type": "string", "description": "关键词过滤（标题、内容预览、会话ID）"},
						"project":  map[string]interface{}{"type": "string", "description": "按项目 ID 筛选"},
						"all":      map[string]interface{}{"type": "boolean", "description": "显示全部会话（包含已归档）"},
						"archived": map[string]interface{}{"type": "boolean", "description": "仅显示已归档会话"},
						"sort":     map[string]interface{}{"type": "string", "enum": []string{"time_desc", "time_asc", "steps_desc", "steps_asc", "title_asc"}, "default": "time_desc"},
						"limit":    map[string]interface{}{"type": "integer", "description": "返回数量上限"},
					},
				},
				OutputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"ok": map[string]interface{}{"type": "boolean"},
						"data": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"items": map[string]interface{}{
									"type": "array",
									"items": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"id":                 map[string]interface{}{"type": "string"},
											"title":              map[string]interface{}{"type": "string"},
											"preview":            map[string]interface{}{"type": "string"},
											"step_count":         map[string]interface{}{"type": "integer"},
											"last_modified_time": map[string]interface{}{"type": "string", "format": "date-time"},
											"workspace_uris":     map[string]interface{}{"type": "string"},
											"status":             map[string]interface{}{"type": "string"},
											"project_id":         map[string]interface{}{"type": "string"},
											"is_archived":        map[string]interface{}{"type": "boolean"},
											"is_running":         map[string]interface{}{"type": "boolean"},
											"pid":                map[string]interface{}{"type": "integer"},
										},
										"required": []string{"id", "title", "step_count", "last_modified_time", "is_archived", "is_running"},
									},
								},
								"total": map[string]interface{}{"type": "integer"},
							},
							"required": []string{"items", "total"},
						},
						"error": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"code":    map[string]interface{}{"type": "string"},
								"message": map[string]interface{}{"type": "string"},
							},
						},
					},
					"required": []string{"ok"},
				},
			},
			"show": {
				Usage:       "agysess show <id>",
				Description: "查看单个会话详细元数据",
				OutputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"ok":   map[string]interface{}{"type": "boolean"},
						"data": map[string]interface{}{"type": "object"},
					},
					"required": []string{"ok"},
				},
			},
			"open": {
				Usage:       "agysess open <id> [--new-window]",
				Description: "在 Windows Terminal 中打开或恢复指定会话",
				OutputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"ok":   map[string]interface{}{"type": "boolean"},
						"data": map[string]interface{}{"type": "object"},
					},
				},
			},
			"new": {
				Usage:       "agysess new [--dir <d>] [--project <p>] [--model <m>]",
				Description: "在 Windows Terminal 中拉起新标签页新建 agy 会话",
				OutputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"ok":   map[string]interface{}{"type": "boolean"},
						"data": map[string]interface{}{"type": "object"},
					},
				},
			},
			"running": {
				Usage:       "agysess running",
				Description: "探测当前活跃运行中的 agy 会话进程",
				OutputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"ok":   map[string]interface{}{"type": "boolean"},
						"data": map[string]interface{}{"type": "array"},
					},
				},
			},
			"kill": {
				Usage:       "agysess kill <id>",
				Description: "关闭指定会话在 Windows Terminal 中的运行中进程/窗口并回收资源",
				OutputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"ok":   map[string]interface{}{"type": "boolean"},
						"data": map[string]interface{}{"type": "object"},
					},
				},
			},
		},
	}
	return json.MarshalIndent(rootSchema, "", "  ")
}
