package protocol

import (
	"encoding/json"
	"strings"
)

// JSONRPCMessage 是通用的 JSON-RPC 2.0 消息信封。
type JSONRPCMessage struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string           `json:"method,omitempty"`
	Params  json.RawMessage  `json:"params,omitempty"`
	Result  json.RawMessage  `json:"result,omitempty"`
	Error   json.RawMessage  `json:"error,omitempty"`
}

// CommandItem 表示 ACP 中的斜杠命令定义。
type CommandItem struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Input       json.RawMessage `json:"input,omitempty"`
}

// SessionNewResult 表示 session/new 响应的最小结果载荷。
type SessionNewResult struct {
	SessionID string `json:"sessionId"`
}

// SessionPromptParams 表示 session/prompt 请求的 params。
type SessionPromptParams struct {
	SessionID string          `json:"sessionId"`
	Prompt    json.RawMessage `json:"prompt"`
}

// ExtractPromptText 从 prompt 字段提取纯文本（支持字符串或内容块数组）。
func ExtractPromptText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	// 尝试按普通字符串解析
	var str string
	if err := json.Unmarshal(raw, &str); err == nil {
		return strings.TrimSpace(str)
	}

	// 尝试按内容块数组解析：[{"type": "text", "text": "..."}]
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &blocks); err == nil {
		var sb strings.Builder
		for _, b := range blocks {
			if b.Text != "" {
				if sb.Len() > 0 {
					sb.WriteString("\n")
				}
				sb.WriteString(b.Text)
			}
		}
		return strings.TrimSpace(sb.String())
	}

	return ""
}

// RewritePrompt 将原始 prompt 中的文本内容替换为 newPromptText，
// 并保持原有结构（普通字符串或内容块数组）。
func RewritePrompt(raw json.RawMessage, newPromptText string) (json.RawMessage, error) {
	if len(raw) == 0 {
		return json.Marshal(newPromptText)
	}

	// 1. 尝试普通字符串
	var str string
	if err := json.Unmarshal(raw, &str); err == nil {
		return json.Marshal(newPromptText)
	}

	// 2. 尝试通用内容块对象数组
	var blocks []map[string]interface{}
	if err := json.Unmarshal(raw, &blocks); err == nil {
		replaced := false
		for _, b := range blocks {
			if t, ok := b["type"].(string); ok && t == "text" {
				b["text"] = newPromptText
				replaced = true
				break
			}
		}
		if !replaced {
			blocks = append([]map[string]interface{}{
				{"type": "text", "text": newPromptText},
			}, blocks...)
		}
		return json.Marshal(blocks)
	}

	// 结构无法识别时回退为普通字符串
	return json.Marshal(newPromptText)
}

// NativeAugmentedCommands contains high-value Antigravity native commands that work via model prompt protocols.
var NativeAugmentedCommands = []CommandItem{
	{
		Name:        "grill-me",
		Description: "对齐需求与交互式方案盘问：在出方案前让 AI 主动提问对齐设计决策",
	},
	{
		Name:        "goal",
		Description: "深度攻坚长任务：保持极致彻底性，反复自检直到目标彻底达成",
	},
	{
		Name:        "compact",
		Description: "立即压缩提炼当前会话历史以释放上下文窗口",
	},
}

// ConfigOptionItem represents one selectable value in an ACP config option.
type ConfigOptionItem struct {
	Value       string `json:"value"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// ConfigOption represents a configuration selector in ACP (rendered in Zed status bar).
type ConfigOption struct {
	ID           string             `json:"id"`
	Name         string             `json:"name"`
	Category     string             `json:"category,omitempty"`
	Type         string             `json:"type"`
	CurrentValue string             `json:"currentValue"`
	Options      []ConfigOptionItem `json:"options,omitempty"`
}

// SetConfigOptionParams represents parameters sent by Zed when user changes a selector.
type SetConfigOptionParams struct {
	SessionID string `json:"sessionId"`
	ConfigID  string `json:"configId"`
	Value     string `json:"value"`
}

// SetModeParams represents params for session/set_mode request.
type SetModeParams struct {
	SessionID string `json:"sessionId"`
	ModeID    string `json:"modeId"`
}

// SessionModeItem represents an available mode in ACP.
type SessionModeItem struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// SessionModes represents the modes payload in session/new and session/load responses.
type SessionModes struct {
	CurrentModeID  string            `json:"currentModeId"`
	AvailableModes []SessionModeItem `json:"availableModes"`
}

// DefaultWorkflowModes defines the Normal vs Plan modes for Zed.
var DefaultWorkflowModes = []SessionModeItem{
	{
		ID:          "normal",
		Name:        "Normal",
		Description: "Normal direct execution mode",
	},
	{
		ID:          "plan",
		Name:        "Plan",
		Description: "Plan handbrake mode (read-only planning, switch to Normal to execute)",
	},
}

// ReplaceModesInResult replaces the top-level modes in rawResult with Workflow modes (Normal/Plan).
func ReplaceModesInResult(rawResult json.RawMessage, currentMode string) (json.RawMessage, error) {
	if len(rawResult) == 0 {
		return rawResult, nil
	}

	var resultMap map[string]json.RawMessage
	if err := json.Unmarshal(rawResult, &resultMap); err != nil {
		return rawResult, err
	}

	if currentMode == "" {
		currentMode = "normal"
	}

	modes := SessionModes{
		CurrentModeID:  currentMode,
		AvailableModes: DefaultWorkflowModes,
	}

	modesBytes, err := json.Marshal(modes)
	if err != nil {
		return rawResult, err
	}

	resultMap["modes"] = modesBytes
	return json.Marshal(resultMap)
}

// RewriteModeConfigOption ensures the "mode" config option exists and has Normal and Plan choices.
func RewriteModeConfigOption(opts []ConfigOption, currentMode string) []ConfigOption {
	if currentMode == "" {
		currentMode = "normal"
	}

	modeChoices := []ConfigOptionItem{
		{
			Value:       "normal",
			Name:        "Normal",
			Description: "Normal conversation and direct execution mode",
		},
		{
			Value:       "plan",
			Name:        "Plan",
			Description: "Plan handbrake mode (read-only planning)",
		},
	}

	// 过滤掉旧的 workflow 选项，避免重复
	var cleaned []ConfigOption
	foundMode := false
	for _, opt := range opts {
		if strings.EqualFold(opt.ID, "workflow") {
			continue
		}
		if strings.EqualFold(opt.ID, "mode") || strings.EqualFold(opt.Category, "mode") {
			opt.ID = "mode"
			opt.Name = "Mode"
			opt.Category = "mode"
			opt.Type = "select"
			opt.CurrentValue = currentMode
			opt.Options = modeChoices
			foundMode = true
		}
		cleaned = append(cleaned, opt)
	}

	if !foundMode {
		cleaned = append(cleaned, ConfigOption{
			ID:           "mode",
			Name:         "Mode",
			Category:     "mode",
			Type:         "select",
			CurrentValue: currentMode,
			Options:      modeChoices,
		})
	}

	return cleaned
}

// RewriteConfigOptionsInResult rewrites or injects the mode config option in rawResult.
func RewriteConfigOptionsInResult(rawResult json.RawMessage, currentMode string) (json.RawMessage, error) {
	if len(rawResult) == 0 {
		return rawResult, nil
	}

	var resultMap map[string]json.RawMessage
	if err := json.Unmarshal(rawResult, &resultMap); err != nil {
		return rawResult, err
	}

	var existing []ConfigOption
	if rawOpts, ok := resultMap["configOptions"]; ok && len(rawOpts) > 0 {
		_ = json.Unmarshal(rawOpts, &existing)
	}

	updated := RewriteModeConfigOption(existing, currentMode)
	updatedBytes, err := json.Marshal(updated)
	if err != nil {
		return rawResult, err
	}

	resultMap["configOptions"] = updatedBytes
	return json.Marshal(resultMap)
}

// DefaultWorkflowConfigOption defines the independent Workflow selector (Normal vs Plan).
var DefaultWorkflowConfigOption = ConfigOption{
	ID:           "workflow",
	Name:         "Workflow",
	Category:     "", // 必须为空：避免未知 category 破坏 Zed 客户端严格枚举反序列化
	Type:         "select",
	CurrentValue: "normal",
	Options: []ConfigOptionItem{
		{
			Value:       "normal",
			Name:        "Normal",
			Description: "Normal conversation and direct execution mode",
		},
		{
			Value:       "plan",
			Name:        "Plan",
			Description: "Automatic planning mode (auto-prepends /plan)",
		},
	},
}

// MergeAvailableCommands 将额外命令合并进 rawResult 的 availableCommands 数组，
// 保留 rawResult 中的全部既有字段并避免重复。
func MergeAvailableCommands(rawResult json.RawMessage, extra []CommandItem) (json.RawMessage, error) {
	if len(rawResult) == 0 {
		return rawResult, nil
	}

	var resultMap map[string]json.RawMessage
	if err := json.Unmarshal(rawResult, &resultMap); err != nil {
		return rawResult, err
	}

	var existing []CommandItem
	if rawCmds, ok := resultMap["availableCommands"]; ok && len(rawCmds) > 0 {
		_ = json.Unmarshal(rawCmds, &existing)
	}

	seen := make(map[string]bool)
	var merged []CommandItem

	for _, c := range existing {
		lower := strings.ToLower(c.Name)
		if !seen[lower] {
			seen[lower] = true
			merged = append(merged, c)
		}
	}

	// 注入解封的官方思维命令
	for _, c := range NativeAugmentedCommands {
		lower := strings.ToLower(c.Name)
		if !seen[lower] {
			seen[lower] = true
			merged = append(merged, c)
		}
	}

	// 注入自定义技能命令
	for _, c := range extra {
		lower := strings.ToLower(c.Name)
		if !seen[lower] {
			seen[lower] = true
			merged = append(merged, c)
		}
	}

	mergedBytes, err := json.Marshal(merged)
	if err != nil {
		return rawResult, err
	}

	resultMap["availableCommands"] = mergedBytes
	return json.Marshal(resultMap)
}

// MergeConfigOptions adds extraOption to configOptions array in rawResult if not already present.
func MergeConfigOptions(rawResult json.RawMessage, extraOption ConfigOption) (json.RawMessage, error) {
	if len(rawResult) == 0 {
		return rawResult, nil
	}

	var resultMap map[string]json.RawMessage
	if err := json.Unmarshal(rawResult, &resultMap); err != nil {
		return rawResult, err
	}

	var existing []ConfigOption
	if rawOpts, ok := resultMap["configOptions"]; ok && len(rawOpts) > 0 {
		_ = json.Unmarshal(rawOpts, &existing)
	}

	found := false
	for _, opt := range existing {
		if strings.EqualFold(opt.ID, extraOption.ID) {
			found = true
			break
		}
	}

	if !found {
		existing = append(existing, extraOption)
	}

	mergedBytes, err := json.Marshal(existing)
	if err != nil {
		return rawResult, err
	}

	resultMap["configOptions"] = mergedBytes
	return json.Marshal(resultMap)
}

// MergeCommandsUpdateNotification 判断 rawParams 是否为 available_commands_update 通知，
// 若是则将额外命令合并进去。
func MergeCommandsUpdateNotification(rawParams json.RawMessage, extra []CommandItem) (json.RawMessage, bool, error) {
	if len(rawParams) == 0 || len(extra) == 0 {
		return rawParams, false, nil
	}

	var paramsMap map[string]json.RawMessage
	if err := json.Unmarshal(rawParams, &paramsMap); err != nil {
		return rawParams, false, err
	}

	updateRaw, ok := paramsMap["update"]
	if !ok || len(updateRaw) == 0 {
		return rawParams, false, nil
	}

	var updateMap map[string]json.RawMessage
	if err := json.Unmarshal(updateRaw, &updateMap); err != nil {
		return rawParams, false, err
	}

	var sessionUpdate string
	if rawSU, ok := updateMap["sessionUpdate"]; ok {
		_ = json.Unmarshal(rawSU, &sessionUpdate)
	}

	rawCmds, hasCommands := updateMap["availableCommands"]
	// 满足显式 sessionUpdate 类型或者直接包含 availableCommands 数组字段
	if sessionUpdate != "available_commands_update" && (!hasCommands || len(rawCmds) == 0) {
		return rawParams, false, nil
	}

	var existing []CommandItem
	if hasCommands && len(rawCmds) > 0 {
		_ = json.Unmarshal(rawCmds, &existing)
	}

	seen := make(map[string]bool)
	var merged []CommandItem

	for _, c := range existing {
		lower := strings.ToLower(c.Name)
		if !seen[lower] {
			seen[lower] = true
			merged = append(merged, c)
		}
	}

	// 合并解封的官方思维命令 (grill-me, goal, compact)
	for _, c := range NativeAugmentedCommands {
		lower := strings.ToLower(c.Name)
		if !seen[lower] {
			seen[lower] = true
			merged = append(merged, c)
		}
	}

	// 合并本地技能命令
	for _, c := range extra {
		lower := strings.ToLower(c.Name)
		if !seen[lower] {
			seen[lower] = true
			merged = append(merged, c)
		}
	}

	mergedBytes, err := json.Marshal(merged)
	if err != nil {
		return rawParams, false, err
	}

	updateMap["availableCommands"] = mergedBytes
	newUpdateRaw, err := json.Marshal(updateMap)
	if err != nil {
		return rawParams, false, err
	}

	paramsMap["update"] = newUpdateRaw
	newParamsRaw, err := json.Marshal(paramsMap)
	if err != nil {
		return rawParams, false, err
	}

	return newParamsRaw, true, nil
}

// SessionUpdateNotification 表示发送给 ACP 客户端（Zed）的 session/update 通知。
type SessionUpdateNotification struct {
	JSONRPC string              `json:"jsonrpc"`
	Method  string              `json:"method"`
	Params  SessionUpdateParams `json:"params"`
}

type SessionUpdateParams struct {
	SessionID string            `json:"sessionId"`
	Update    SessionInfoUpdate `json:"update"`
}

type SessionInfoUpdate struct {
	SessionUpdate string `json:"sessionUpdate"`
	Title         string `json:"title"`
}
