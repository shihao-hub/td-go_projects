// Package contract 定义 sourcecount 的 CLI、MCP 与公共 Server 共用的数据契约。
package contract

// ScanRequest 是一次扫描请求。Roots 为空时由 Server 使用当前工作目录。
type ScanRequest struct {
	Roots             []string `json:"roots,omitempty" jsonschema:"扫描根路径；缺省为当前工作目录"`
	ConfigPath        string   `json:"config_path,omitempty" jsonschema:"显式 JSON 配置文件路径；缺省按约定自动发现"`
	Include           []string `json:"include,omitempty" jsonschema:"替换配置文件 include 的 Glob 列表"`
	Exclude           []string `json:"exclude,omitempty" jsonschema:"替换配置文件 exclude 的 Glob 列表"`
	TextExtensions    []string `json:"text_extensions,omitempty" jsonschema:"强制按文本处理的后缀列表"`
	BinaryExtensions  []string `json:"binary_extensions,omitempty" jsonschema:"强制按二进制处理的后缀列表"`
	NoDefaultExcludes bool     `json:"no_default_excludes,omitempty" jsonschema:"是否关闭内置默认排除目录"`
}

// ScanReport 是成功或部分成功扫描的公开结果。
type ScanReport struct {
	Roots      []string         `json:"roots"`
	ConfigPath string           `json:"config_path,omitempty"`
	Groups     []ExtensionGroup `json:"groups"`
	Totals     Totals           `json:"totals"`
	Errors     []ScanError      `json:"errors"`
}

// ExtensionGroup 是一个后缀的聚合结果。同一后缀允许同时出现文本和二进制文件。
type ExtensionGroup struct {
	Extension string      `json:"extension"`
	Text      TextStats   `json:"text"`
	Binary    BinaryStats `json:"binary"`
}

// TextStats 是文本文件指标。
type TextStats struct {
	Files int64 `json:"files"`
	Lines int64 `json:"lines"`
	Chars int64 `json:"chars"`
}

// BinaryStats 是二进制文件指标。
type BinaryStats struct {
	Files int64 `json:"files"`
	Bytes int64 `json:"bytes"`
}

// Totals 是所有后缀的合计。
type Totals struct {
	Text   TextStats   `json:"text"`
	Binary BinaryStats `json:"binary"`
}

// ScanError 是单个文件的可公开错误记录。
type ScanError struct {
	Path    string `json:"path"`
	Code    string `json:"code"`
	Message string `json:"message"`
}
