package model

import "encoding/json"

// ExportSchema 返回工具输出的 JSON Schema 契约字符串（零 I/O 静态导出）。
func ExportSchema() string {
	schema := map[string]interface{}{
		"$schema":     "http://json-schema.org/draft-07/schema#",
		"title":       "agyapptitle Output Envelope",
		"description": "Standard output format for Antigravity session titles governance daemon",
		"type":        "object",
		"properties": map[string]interface{}{
			"version": map[string]interface{}{
				"type":        "string",
				"description": "Component version",
			},
			"status": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"ok", "error", "running", "stopped"},
				"description": "Execution status",
			},
			"data": map[string]interface{}{
				"type": "object",
			},
			"error": map[string]interface{}{
				"type": "string",
			},
		},
		"required": []string{"status"},
	}

	b, _ := json.MarshalIndent(schema, "", "  ")
	return string(b)
}
