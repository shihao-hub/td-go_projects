// Package store 负责 SQLite 存储：打开/迁移/CRUD/统计。
package store

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// meta 约束常量（应用层强制，单点收口在 ValidateMeta）
const (
	metaMaxBytes      = 4096 // 序列化后总字节数硬限
	metaSourceMaxLen  = 64   // source 最大字节数
	metaTagsMaxCount  = 8    // tags 最多项数
	metaTagMaxLen     = 32   // 单个 tag 最大字节数
	metaGroupMaxCount = 8    // group 最多项数
	metaGroupMaxLen   = 32   // 单个分组名最大字节数
)

// MetaError meta 校验错误，Code 即 JSON 错误码：
// meta_unknown_key / meta_invalid / meta_too_large
type MetaError struct {
	Code    string
	Message string
}

func (e *MetaError) Error() string { return e.Message }

// ValidateMeta 校验并规范化 meta JSON。
// 规则：
//   - 空串 / null / {} → 存储 NULL（返回 nil）
//   - 必须是 JSON 对象；key 白名单：source（string ≤64B）、tags（[]string ≤8 项、每项 ≤32B）、
//     group（string 或 []string，≤8 项、每项 ≤32B，首尾空白裁剪）
//   - 未知 key → meta_unknown_key；类型/长度不符 → meta_invalid；规范化后 >4KB → meta_too_large
//   - tags 去重后小写存储并排序，保证同输入同存储
//   - group 写入时统一归一化为数组：去重（大小写敏感）后排序；空值/空数组剔除整个 key
func ValidateMeta(raw []byte) (json.RawMessage, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil, nil
	}

	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, &MetaError{Code: "meta_invalid", Message: "meta 必须是 JSON 对象: " + err.Error()}
	}

	out := make(map[string]any, len(obj))
	for key, val := range obj {
		switch key {
		case "source":
			var s string
			if err := json.Unmarshal(val, &s); err != nil {
				return nil, &MetaError{Code: "meta_invalid", Message: "source 必须是字符串"}
			}
			if len(s) > metaSourceMaxLen {
				return nil, &MetaError{Code: "meta_invalid", Message: fmt.Sprintf("source 超长（≤%d 字节）", metaSourceMaxLen)}
			}
			out["source"] = s
		case "tags":
			var tags []string
			if err := json.Unmarshal(val, &tags); err != nil {
				return nil, &MetaError{Code: "meta_invalid", Message: "tags 必须是字符串数组"}
			}
			if len(tags) > metaTagsMaxCount {
				return nil, &MetaError{Code: "meta_invalid", Message: fmt.Sprintf("tags 数量超限（≤%d 个）", metaTagsMaxCount)}
			}
			seen := make(map[string]struct{}, len(tags))
			dedup := make([]string, 0, len(tags))
			for _, t := range tags {
				if len(t) > metaTagMaxLen {
					return nil, &MetaError{Code: "meta_invalid", Message: fmt.Sprintf("单个 tag 超长（≤%d 字节）", metaTagMaxLen)}
				}
				lower := strings.ToLower(t)
				if _, dup := seen[lower]; !dup {
					seen[lower] = struct{}{}
					dedup = append(dedup, lower)
				}
			}
			if len(dedup) > 0 {
				sort.Strings(dedup)
				out["tags"] = dedup
			}
		case "group":
			groups, err := parseGroups(val)
			if err != nil {
				return nil, err
			}
			norm, err := normalizeGroups(groups)
			if err != nil {
				return nil, err
			}
			if len(norm) > 0 {
				out["group"] = norm
			}
		default:
			return nil, &MetaError{Code: "meta_unknown_key", Message: fmt.Sprintf("未知的 meta key: %q（白名单: source, tags, group）", key)}
		}
	}

	if len(out) == 0 {
		return nil, nil
	}
	norm, err := json.Marshal(out)
	if err != nil {
		return nil, &MetaError{Code: "meta_invalid", Message: "meta 序列化失败"}
	}
	if len(norm) > metaMaxBytes {
		return nil, &MetaError{Code: "meta_too_large", Message: fmt.Sprintf("meta 序列化后超过 %d 字节", metaMaxBytes)}
	}
	return norm, nil
}

// parseGroups 解析 group 原值：兼容单字符串（旧数据/旧调用）与字符串数组。
func parseGroups(raw json.RawMessage) ([]string, error) {
	var single string
	if err := json.Unmarshal(raw, &single); err == nil {
		return []string{single}, nil
	}
	var list []string
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, &MetaError{Code: "meta_invalid", Message: "group 必须是字符串或字符串数组"}
	}
	return list, nil
}

// normalizeGroups 规范化分组数组：trim → 剔空 → 单项长度 → 精确去重 → 排序 → 数量上限。
// 去重大小写敏感（保留存量分组名如 "Dev" 的原样语义）。
func normalizeGroups(groups []string) ([]string, error) {
	seen := make(map[string]struct{}, len(groups))
	out := make([]string, 0, len(groups))
	for _, raw := range groups {
		g := strings.TrimSpace(raw)
		if g == "" {
			continue
		}
		if len(g) > metaGroupMaxLen {
			return nil, &MetaError{Code: "meta_invalid", Message: fmt.Sprintf("group 超长（≤%d 字节）", metaGroupMaxLen)}
		}
		if _, dup := seen[g]; dup {
			continue
		}
		seen[g] = struct{}{}
		out = append(out, g)
	}
	if len(out) > metaGroupMaxCount {
		return nil, &MetaError{Code: "meta_invalid", Message: fmt.Sprintf("group 数量超限（≤%d 个）", metaGroupMaxCount)}
	}
	sort.Strings(out)
	return out, nil
}
