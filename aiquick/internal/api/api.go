// Package api 定义前后端共享的业务 DTO（请求参数与结果结构）。
// 只放纯类型，不含任何逻辑，两侧共同 import 以保证 JSON 契约一致。
package api

// Config LLM 访问配置，持久化于 config.json。
type Config struct {
	BaseURL string `json:"baseURL"`
	APIKey  string `json:"apiKey"`
	Model   string `json:"model"`
}

// Preset 提示词预设，持久化于 presets.json。
// System 为给模型的指令；UserTemplate 可选，含 {{input}} 占位符，
// 缺省时用户输入直接作为 user 消息。
type Preset struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	System       string `json:"system"`
	UserTemplate string `json:"userTemplate,omitempty"`
}

// HelloResult hello 握手的应答。
type HelloResult struct {
	Proto   int    `json:"proto"`
	Name    string `json:"name"`
	Version string `json:"version"`
}

// AskParams ask.stream 请求参数。
type AskParams struct {
	PresetID string `json:"presetId"`
	Input    string `json:"input"`
	// System 非空时覆盖预设中的指令（预留，正常流程不用）。
	System string `json:"system,omitempty"`
}

// AskResult ask.stream 完成后的最终应答。
type AskResult struct {
	Text string `json:"text"`
}

// ChunkData chunk 事件的数据载荷（流式增量文本）。
type ChunkData struct {
	Text string `json:"text"`
}

// CancelParams ask.cancel 请求参数。
type CancelParams struct {
	RID int64 `json:"rid"`
}

// PresetIDParams presets.delete 请求参数。
type PresetIDParams struct {
	ID string `json:"id"`
}

// 事件名。
const (
	EventChunk = "chunk"
)
