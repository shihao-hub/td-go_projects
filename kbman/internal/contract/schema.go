package contract

import (
	"reflect"
	"strings"

	"kbman/internal/service"
)

// Schema derives a JSON Schema (2020-12 subset) for a Go value's JSON shape.
// It keeps the CLI contract in sync with the structs it actually emits instead
// of maintaining a hand-written copy.
func Schema(v any) map[string]any {
	return schemaOf(reflect.TypeOf(v), map[reflect.Type]bool{})
}

func schemaOf(t reflect.Type, seen map[reflect.Type]bool) map[string]any {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	switch t.Kind() {
	case reflect.Struct:
		if seen[t] {
			return map[string]any{"type": "object"}
		}
		seen[t] = true
		defer delete(seen, t)

		props := map[string]any{}
		required := []string{}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if !f.IsExported() {
				continue
			}
			tag := f.Tag.Get("json")
			name, opts, _ := strings.Cut(tag, ",")
			if name == "" || name == "-" {
				name = strings.ToLower(f.Name)
			}
			props[name] = schemaOf(f.Type, seen)
			if !strings.Contains(opts, "omitempty") {
				required = append(required, name)
			}
		}

		out := map[string]any{
			"type":                 "object",
			"properties":           props,
			"additionalProperties": false,
		}
		if len(required) > 0 {
			out["required"] = required
		}
		return out

	case reflect.Slice, reflect.Array:
		return map[string]any{
			"type":  "array",
			"items": schemaOf(t.Elem(), seen),
		}

	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return map[string]any{"type": "integer"}
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}
	case reflect.Interface:
		return map[string]any{}
	default:
		return map[string]any{}
	}
}

type command struct {
	name        string
	description string
	inputs      map[string]any
	output      any
}

func commands() []command {
	return []command{
		{
			name:        "migrate",
			description: "批量扫描知识库并注入时序元数据，只追加缺失字段，保留原文逐字不变",
			inputs: objectSchema(map[string]any{
				"dir":   map[string]any{"type": "string", "description": "知识库目录路径"},
				"force": map[string]any{"type": "boolean", "description": "覆盖已有元数据字段"},
				"json":  map[string]any{"type": "boolean", "description": "输出 JSON 包络"},
			}, "dir"),
			output: service.MigrationResult{},
		},
		{
			name:        "check",
			description: "前置检查条目状态，verified/suspect/draft/deprecated 对应不同执行建议与退出码",
			inputs: objectSchema(map[string]any{
				"dir":    map[string]any{"type": "string"},
				"target": map[string]any{"type": "string", "description": "条目 ID（如 002）或完整文件名"},
				"json":   map[string]any{"type": "boolean"},
			}, "target"),
			output: service.CheckReport{},
		},
		{
			name:        "report",
			description: "执行期被报错证伪时，原子更新状态并追加自愈排障日志",
			inputs: objectSchema(map[string]any{
				"dir":    map[string]any{"type": "string"},
				"target": map[string]any{"type": "string"},
				"status": map[string]any{"type": "string", "enum": []string{"verified", "suspect", "deprecated"}},
				"reason": map[string]any{"type": "string"},
				"append": map[string]any{"type": "string"},
			}, "target"),
			output: mutationResult{},
		},
		{
			name:        "touch",
			description: "实操验证通过后刷新 last_verified 时间戳",
			inputs: objectSchema(map[string]any{
				"dir":    map[string]any{"type": "string"},
				"target": map[string]any{"type": "string"},
			}, "target"),
			output: mutationResult{},
		},
	}
}

func objectSchema(props map[string]any, required ...string) map[string]any {
	out := map[string]any{
		"type":                 "object",
		"properties":           props,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		out["required"] = required
	}
	return out
}

// Catalog returns the offline `kbman schema` document (interface: cli).
func Catalog(name, version string) map[string]any {
	tools := make([]map[string]any, 0, len(commands()))
	for _, c := range commands() {
		tools = append(tools, map[string]any{
			"name":         c.name,
			"description":  c.description,
			"inputSchema":  c.inputs,
			"outputSchema": envelope(Schema(c.output)),
		})
	}

	return map[string]any{
		"name":      name,
		"version":   version,
		"interface": "cli",
		"tools":     tools,
	}
}

// CommandContract returns the input and (enveloped) output schema of a subcommand.
func CommandContract(name string) (map[string]any, bool) {
	for _, c := range commands() {
		if c.name == name {
			return map[string]any{
				"name":         c.name,
				"inputSchema":  c.inputs,
				"outputSchema": envelope(Schema(c.output)),
			}, true
		}
	}
	return nil, false
}

// envelope mirrors the CLI JSON envelope: {"ok": true, "data": ...}.
func envelope(data map[string]any) map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ok":   map[string]any{"type": "boolean"},
			"data": data,
		},
		"required": []string{"ok", "data"},
	}
}

type mutationResult struct {
	Entry  string `json:"entry"`
	Action string `json:"action"`
	Status string `json:"status,omitempty"`
}
