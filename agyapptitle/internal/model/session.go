package model

import (
	"regexp"
	"strings"
	"unicode"
)

var (
	tagPattern  = regexp.MustCompile(`^[\[【](.*?)[\]】]\s*`)
	enWordRegex = regexp.MustCompile(`[a-zA-Z]{2,}`)
)

// SessionRecord 表示从数据库中读取的单条会话摘要记录。
type SessionRecord struct {
	ID           string `json:"id"`
	Env          string `json:"env"`
	Title        string `json:"title"`
	Preview      string `json:"preview"`
	StepCount    int    `json:"step_count"`
	LastModified string `json:"last_modified"`
	Workspace    string `json:"workspace"`
	HasRaw       bool   `json:"has_raw"`
	IsChinese    bool   `json:"is_chinese"`
}

// RenameTask 表示待执行的标题改写任务。
type RenameTask struct {
	SessionID string `json:"session_id"`
	OldTitle  string `json:"old_title"`
	NewTitle  string `json:"new_title"`
	Preview   string `json:"preview"`
	Env       string `json:"env"`
}

// IsChineseTitle 严格判断标题主体是否为合格中文，
// 剥离分类前缀（如【待办】、[WIP]）后研判汉字与英文单词占比。
func IsChineseTitle(title string) bool {
	title = strings.TrimSpace(title)
	if title == "" {
		return false
	}

	// 排除模型在空提问下产生的异常幻觉提示语
	if strings.Contains(title, "请提供") || strings.Contains(title, "无法生成") || strings.Contains(title, "未提供") || strings.Contains(title, "抱歉") {
		return false
	}

	stripped := tagPattern.ReplaceAllString(title, "")
	stripped = strings.TrimSpace(stripped)
	if stripped == "" {
		return true
	}

	var cnCount int
	for _, r := range stripped {
		if unicode.Is(unicode.Han, r) {
			cnCount++
		}
	}

	if cnCount == 0 {
		return false
	}

	enWords := enWordRegex.FindAllString(stripped, -1)
	if len(enWords) >= 3 && cnCount <= 1 {
		return false
	}

	return true
}

// SanitizePreview 清理首条 Prompt 预览文本（去除回车换行与多余空白）。
func SanitizePreview(preview string) string {
	preview = strings.ReplaceAll(preview, "\r", " ")
	preview = strings.ReplaceAll(preview, "\n", " ")
	return strings.Join(strings.Fields(preview), " ")
}
