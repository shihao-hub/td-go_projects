package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// effortConfig: providerID → modelID → 写死在模型 options 里的思考档位。
// 这类模型（如 zhipu-glm/glm-5.3 的 reasoningEffort: max）没有档位变体，
// DB 里 variant 恒为 default/缺失，真实档位需要从 opencode 配置合并。
type effortConfig map[string]map[string]string

// loadEffortConfig 从 opencode 全局配置提取每个模型 options 里固定的
// reasoningEffort / effort。与 opencode 的加载顺序一致（config.json →
// opencode.json → opencode.jsonc），后加载的覆盖先加载的。
// 配置缺失或解析失败（如 jsonc 带注释）不影响主流程，跳过即可。
func loadEffortConfig() (effortConfig, bool) {
	cfg := effortConfig{}
	dir := os.Getenv("OPENCODE_CONFIG")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return cfg, false
		}
		dir = filepath.Join(home, ".config", "opencode")
	}
	for _, name := range []string{"config.json", "opencode.json", "opencode.jsonc"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		var doc struct {
			Provider map[string]struct {
				Models map[string]struct {
					Options struct {
						ReasoningEffort string `json:"reasoningEffort"`
						Effort          string `json:"effort"`
					} `json:"options"`
				} `json:"models"`
			} `json:"provider"`
		}
		if json.Unmarshal(data, &doc) != nil {
			continue
		}
		for pid, models := range doc.Provider {
			for mid, m := range models.Models {
				eff := m.Options.ReasoningEffort
				if eff == "" {
					eff = m.Options.Effort
				}
				if eff == "" {
					continue
				}
				if cfg[pid] == nil {
					cfg[pid] = map[string]string{}
				}
				cfg[pid][mid] = eff
			}
		}
	}
	return cfg, len(cfg) > 0
}

func (c effortConfig) lookup(provider, model string) string {
	if c == nil {
		return ""
	}
	if models, ok := c[provider]; ok {
		return models[model]
	}
	return ""
}
