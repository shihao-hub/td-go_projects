// Package feishu 构建飞书交互卡片，并通过群自定义机器人 Webhook 投递。
package feishu

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// 卡片标题栏配色。
const (
	TemplateBlue   = "blue"
	TemplateGreen  = "green"
	TemplateOrange = "orange"
	TemplateRed    = "red"
	TemplateGrey   = "grey"
)

const (
	maxTitleRunes   = 80
	maxSummaryRunes = 1500
)

// MetaField 是卡片正文中的一行"标签：值"。
type MetaField struct {
	Label string
	Value string
}

// CardOptions 描述一张邮件提醒卡片。
type CardOptions struct {
	Title    string
	Template string
	Sender   string
	Subject  string
	Meta     []MetaField
	Summary  string
	LinkURL  string
	LinkText string
}

// 以下是飞书卡片 2.0（schema 2.0）的 wire 结构。字段保持最小集，
// 避免本地结构体与飞书文档漂移时静默丢字段。

type cardObject struct {
	Schema string     `json:"schema"`
	Config cardConfig `json:"config"`
	Header cardHeader `json:"header"`
	Body   cardBody   `json:"body"`
}

type cardConfig struct {
	UpdateMulti bool `json:"update_multi"`
}

type cardHeader struct {
	Title    textNode `json:"title"`
	Template string   `json:"template"`
}

type textNode struct {
	Tag     string `json:"tag"`
	Content string `json:"content"`
}

type cardBody struct {
	Elements []any `json:"elements"`
}

type markdownElement struct {
	Tag     string `json:"tag"`
	Content string `json:"content"`
}

type actionElement struct {
	Tag     string       `json:"tag"`
	Actions []buttonNode `json:"actions"`
}

type buttonNode struct {
	Tag  string   `json:"tag"`
	Text textNode `json:"text"`
	Type string   `json:"type"`
	URL  string   `json:"url"`
}

// BuildCard 把邮件信息转换为飞书卡片 2.0 结构。
func BuildCard(opts CardOptions) cardObject {
	template := opts.Template
	if template == "" {
		template = TemplateBlue
	}
	title := strings.TrimSpace(opts.Title)
	if title == "" {
		title = "新邮件提醒"
	}

	elements := make([]any, 0, 3)
	if body := buildBodyMarkdown(opts); body != "" {
		elements = append(elements, markdownElement{Tag: "markdown", Content: body})
	}
	if opts.LinkURL != "" {
		text := strings.TrimSpace(opts.LinkText)
		if text == "" {
			text = "打开链接"
		}
		elements = append(elements, actionElement{
			Tag: "action",
			Actions: []buttonNode{{
				Tag:  "button",
				Text: textNode{Tag: "plain_text", Content: text},
				Type: "primary",
				URL:  opts.LinkURL,
			}},
		})
	}

	return cardObject{
		Schema: "2.0",
		Config: cardConfig{UpdateMulti: true},
		Header: cardHeader{
			Title:    textNode{Tag: "plain_text", Content: truncateRunes(title, maxTitleRunes)},
			Template: template,
		},
		Body: cardBody{Elements: elements},
	}
}

func buildBodyMarkdown(opts CardOptions) string {
	lines := make([]string, 0, len(opts.Meta)+3)
	if sender := strings.TrimSpace(opts.Sender); sender != "" {
		lines = append(lines, fmt.Sprintf("**发件人**：%s", sender))
	}
	if subject := strings.TrimSpace(opts.Subject); subject != "" {
		lines = append(lines, fmt.Sprintf("**主题**：%s", subject))
	}
	for _, field := range opts.Meta {
		label := strings.TrimSpace(field.Label)
		value := strings.TrimSpace(field.Value)
		if label == "" || value == "" {
			continue
		}
		lines = append(lines, fmt.Sprintf("**%s**：%s", label, value))
	}
	if summary := strings.TrimSpace(opts.Summary); summary != "" {
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, truncateRunes(summary, maxSummaryRunes))
	}
	return strings.Join(lines, "\n")
}

// truncateRunes 按字符截断，保证不会切断 UTF-8 编码。
func truncateRunes(value string, limit int) string {
	if limit <= 0 || utf8.RuneCountInString(value) <= limit {
		return value
	}
	runes := []rune(value)
	return string(runes[:limit]) + "…"
}
