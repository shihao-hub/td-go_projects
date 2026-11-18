package cli

import (
	"encoding/json"
	"flag"
	"fmt"

	"ccquota/internal/appenv"
	"ccquota/internal/ccswitch"
	"ccquota/internal/cred"
)

// checkResult 是 doctor 的单项检查结果。
type checkResult struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

// runDoctor 分层自检：库可读性 → 凭据存在性。
func runDoctor(args []string, opt *Options) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(opt.Stderr)
	asJSON := fs.Bool("json", false, "输出 JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	var checks []checkResult
	add := func(name string, ok bool, detail string) {
		checks = append(checks, checkResult{Name: name, OK: ok, Detail: detail})
	}

	dbPath := opt.DBPath
	if dbPath == "" {
		p, err := ccswitch.DefaultDBPath()
		if err != nil {
			add("cc-switch 库", false, err.Error())
		} else {
			dbPath = p
		}
	}
	if dbPath != "" {
		providers, err := ccswitch.Load(dbPath)
		if err != nil {
			add("cc-switch 库", false, err.Error())
		} else {
			eligible := 0
			for _, p := range providers {
				if p.Usage != nil || p.AppType == "kimi" || p.AppType == "agy" || p.AppType == "codex" {
					eligible++
				}
			}
			add("cc-switch 库", true, fmt.Sprintf("读取到 %d 个供应商，其中 %d 个可能展示额度", len(providers), eligible))
		}
	}

	if _, err := cred.LoadCodexToken(); err != nil {
		add("Codex 凭据", false, err.Error())
	} else {
		add("Codex 凭据", true, "已找到 ~/.codex/auth.json 的 access_token")
	}

	if _, err := cred.LoadKimiToken("managed:kimi-code"); err != nil {
		add("Kimi 凭据", false, err.Error())
	} else {
		add("Kimi 凭据", true, "已找到 ~/.cc-switch/kimi_accounts.json 的 token")
	}

	if _, ok := appenv.DiscoverRuntime(); ok {
		add("运行实例", true, "存在 runtime.json（ccquota web 正在运行）")
	} else {
		add("运行实例", true, "当前无运行中的 WebUI（不影响 list）")
	}

	failed := false
	for _, c := range checks {
		if !c.OK {
			failed = true
		}
	}

	if *asJSON {
		writeJSON(opt.Stdout, envelope{OK: !failed, Data: map[string]any{"checks": checks}})
		if failed {
			return 1
		}
		return 0
	}

	for _, c := range checks {
		mark := "✓"
		if !c.OK {
			mark = "✗"
		}
		fmt.Fprintf(opt.Stdout, "%s %s: %s\n", mark, c.Name, c.Detail)
	}
	if failed {
		return 1
	}
	return 0
}

// runSchema 输出 JSON 契约目录；此分支不访问数据库与网络。
func runSchema(args []string, opt *Options) int {
	_ = args
	schema := map[string]any{
		"name":    "ccquota",
		"version": opt.Version,
		"commands": []map[string]any{
			{
				"name":        "list",
				"description": "列出 cc-switch 中已启用额度查询的供应商额度",
				"flags": []string{"--json", "--app", "--refresh"},
				"output":      "envelope<{cards:[Card], updatedAt:string, fromCache:bool}>",
			},
			{
				"name":        "doctor",
				"description": "分层自检：库可读性、凭据存在性",
				"flags":       []string{"--json"},
				"output":      "envelope<{checks:[{name,ok,detail}]}>",
			},
			{"name": "schema", "description": "输出本契约目录", "output": "schema"},
			{"name": "url", "description": "输出运行中的 WebUI 地址", "output": "text"},
		},
		"card": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"app":        map[string]any{"type": "string"},
				"providerId": map[string]any{"type": "string"},
				"name":       map[string]any{"type": "string"},
				"baseUrl":    map[string]any{"type": "string"},
				"kind":       map[string]any{"type": "string", "enum": []string{"coding_plan", "balance", "subscription"}},
				"current":    map[string]any{"type": "boolean"},
				"ok":         map[string]any{"type": "boolean"},
				"tiers": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"key":         map[string]any{"type": "string", "enum": []string{"five_hour", "weekly", "monthly"}},
							"label":       map[string]any{"type": "string"},
							"utilization": map[string]any{"type": "number", "description": "已用百分比 0-100"},
							"resetsAt":    map[string]any{"type": "string", "format": "date-time"},
						},
					},
				},
				"balance": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"value": map[string]any{"type": "number"},
						"unit":  map[string]any{"type": "string"},
					},
				},
				"err":       map[string]any{"type": "string"},
				"queriedAt": map[string]any{"type": "string", "format": "date-time"},
			},
		},
	}
	enc := json.NewEncoder(opt.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(schema)
	return 0
}
