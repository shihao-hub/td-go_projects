package model

// SchemaDoc 返回 CLI 的静态契约目录（interface=cli），不触发任何业务 I/O。
func SchemaDoc(version string) map[string]any {
	number := map[string]any{"type": "number"}
	integer := map[string]any{"type": "integer"}
	boolean := map[string]any{"type": "boolean"}
	str := map[string]any{"type": "string"}

	configSchema := object(map[string]any{
		"min_uptime_hours":   number,
		"min_memory_percent": number,
		"force_uptime_hours": number,
		"snooze_short_hours": number,
		"snooze_long_hours":  number,
	}, "min_uptime_hours", "min_memory_percent", "force_uptime_hours", "snooze_short_hours", "snooze_long_hours")

	processSchema := object(map[string]any{
		"name":              str,
		"working_set_bytes": integer,
		"percent":           number,
	}, "name", "working_set_bytes", "percent")

	memorySchema := object(map[string]any{
		"total_bytes": integer,
		"used_bytes":  integer,
		"percent":     number,
	}, "total_bytes", "used_bytes", "percent")

	snapshotSchema := object(map[string]any{
		"uptime_seconds": integer,
		"memory":         ref("memory"),
		"top_processes":  arrayOf(ref("process")),
	}, "uptime_seconds", "memory", "top_processes")

	thresholdsSchema := object(map[string]any{
		"min_uptime_hours":   number,
		"min_memory_percent": number,
		"force_uptime_hours": number,
	}, "min_uptime_hours", "min_memory_percent", "force_uptime_hours")

	decisionSchema := object(map[string]any{
		"should_notify": boolean,
		"trigger":       str,
		"reason":        str,
	}, "should_notify", "reason")

	statusSchema := object(map[string]any{
		"uptime_seconds": integer,
		"memory":         ref("memory"),
		"top_processes":  arrayOf(ref("process")),
		"thresholds":     ref("thresholds"),
		"snooze_until":   str,
		"snooze_active":  boolean,
		"would_notify":   boolean,
		"health":         map[string]any{"type": "string", "enum": []string{"healthy", "watch", "attention"}},
	}, "uptime_seconds", "memory", "top_processes", "thresholds", "snooze_active", "would_notify", "health")

	checkSchema := object(map[string]any{
		"notified":          boolean,
		"forced":            boolean,
		"action":            str,
		"restart_requested": boolean,
		"decision":          ref("decision"),
		"snapshot":          ref("snapshot"),
		"snooze_until":      str,
	}, "notified", "forced", "restart_requested", "decision", "snapshot")

	taskSchema := object(map[string]any{
		"name":      str,
		"installed": boolean,
		"exe_path":  str,
		"next_run":  str,
	}, "name", "installed", "exe_path")

	return map[string]any{
		"name":        "rebootremind",
		"version":     version,
		"interface":   "cli",
		"description": "Windows 开机时长与内存健康巡检重启提醒工具",
		"data_dir":    `%APPDATA%\language_projects\rebootremind\<dev|prod>`,
		"commands": []any{
			command("status", "查询当前系统健康状态与提醒判定", object(nil), envelopeRef("status")),
			command("check", "执行巡检；--notify 时在命中规则后弹出原生弹窗", object(map[string]any{
				"notify": map[string]any{"type": "boolean", "default": false},
				"force":  map[string]any{"type": "boolean", "default": false},
			}), envelopeRef("check_result")),
			command("task install", "注册每小时巡检的计划任务", object(map[string]any{
				"force": map[string]any{"type": "boolean", "default": false},
			}), envelopeRef("task")),
			command("task uninstall", "卸载巡检计划任务", object(nil), envelopeRef("task")),
			command("task status", "查询计划任务是否已安装", object(nil), envelopeRef("task")),
			command("config show", "查看当前配置", object(nil), envelopeRef("config")),
			command("config set", "修改单个配置项（config set <key> <value>）", object(map[string]any{
				"key":   str,
				"value": number,
			}, "key", "value"), envelopeRef("config")),
			command("schema", "导出本静态契约目录", object(nil), map[string]any{"type": "object"}),
			command("version", "输出版本信息", object(nil), map[string]any{"type": "object"}),
		},
		"$defs": map[string]any{
			"config":       configSchema,
			"process":      processSchema,
			"memory":       memorySchema,
			"snapshot":     snapshotSchema,
			"thresholds":   thresholdsSchema,
			"decision":     decisionSchema,
			"status":       statusSchema,
			"check_result": checkSchema,
			"task":         taskSchema,
		},
	}
}

func command(name, description string, input, output map[string]any) map[string]any {
	return map[string]any{
		"name":          name,
		"description":   description,
		"input_schema":  input,
		"output_schema": output,
	}
}

func object(properties map[string]any, required ...string) map[string]any {
	if properties == nil {
		properties = map[string]any{}
	}
	result := map[string]any{
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		result["required"] = required
	}
	return result
}

func arrayOf(items map[string]any) map[string]any {
	return map[string]any{"type": "array", "items": items}
}

func ref(name string) map[string]any {
	return map[string]any{"$ref": "#/$defs/" + name}
}

// envelopeRef 构造成功包络的输出 Schema；失败分支形状由 CLI 统一约定。
func envelopeRef(dataName string) map[string]any {
	return object(map[string]any{
		"ok":   booleanSchema(),
		"data": ref(dataName),
		"error": object(map[string]any{
			"code":    map[string]any{"type": "string"},
			"message": map[string]any{"type": "string"},
		}, "code", "message"),
	}, "ok")
}

func booleanSchema() map[string]any {
	return map[string]any{"type": "boolean"}
}
