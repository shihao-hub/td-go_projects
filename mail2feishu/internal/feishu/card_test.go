package feishu

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildCardDefaults(t *testing.T) {
	card := BuildCard(CardOptions{})
	if card.Schema != "2.0" {
		t.Errorf("Schema = %q, want 2.0", card.Schema)
	}
	if card.Header.Title.Content != "新邮件提醒" {
		t.Errorf("默认标题 = %q", card.Header.Title.Content)
	}
	if card.Header.Template != TemplateBlue {
		t.Errorf("默认配色 = %q, want %q", card.Header.Template, TemplateBlue)
	}
	if len(card.Body.Elements) != 0 {
		t.Errorf("空信息不应产生正文元素，got %d", len(card.Body.Elements))
	}
}

func TestBuildCardContent(t *testing.T) {
	card := BuildCard(CardOptions{
		Title:    "GitHub 新 Issue",
		Template: TemplateGreen,
		Sender:   "GitHub <notifications@github.com>",
		Subject:  "[owner/repo] New issue",
		Meta: []MetaField{
			{Label: "仓库", Value: "owner/repo"},
			{Label: "讨论串", Value: "#42"},
		},
		Summary:  "有人提了一个新 issue",
		LinkURL:  "https://github.com/owner/repo/issues/42",
		LinkText: "查看 Issue",
	})

	if card.Header.Template != TemplateGreen {
		t.Errorf("Template = %q", card.Header.Template)
	}
	if len(card.Body.Elements) != 2 {
		t.Fatalf("Elements 数量 = %d, want 2", len(card.Body.Elements))
	}
	markdown, ok := card.Body.Elements[0].(markdownElement)
	if !ok {
		t.Fatalf("首个元素类型 = %T, want markdownElement", card.Body.Elements[0])
	}
	for _, want := range []string{"**发件人**：GitHub", "**主题**：", "**仓库**：owner/repo", "**讨论串**：#42", "有人提了一个新 issue"} {
		if !strings.Contains(markdown.Content, want) {
			t.Errorf("正文缺少 %q:\n%s", want, markdown.Content)
		}
	}

	action, ok := card.Body.Elements[1].(actionElement)
	if !ok {
		t.Fatalf("第二个元素类型 = %T, want actionElement", card.Body.Elements[1])
	}
	if len(action.Actions) != 1 || action.Actions[0].URL != "https://github.com/owner/repo/issues/42" {
		t.Errorf("按钮 = %+v", action.Actions)
	}
	if action.Actions[0].Text.Content != "查看 Issue" {
		t.Errorf("按钮文案 = %q", action.Actions[0].Text.Content)
	}
}

func TestBuildCardOmitsEmptyMetaFields(t *testing.T) {
	card := BuildCard(CardOptions{
		Subject: "主题",
		Meta: []MetaField{
			{Label: "仓库", Value: " "},
			{Label: "", Value: "x"},
		},
	})
	markdown, ok := card.Body.Elements[0].(markdownElement)
	if !ok {
		t.Fatal("应包含 markdown 元素")
	}
	if strings.Contains(markdown.Content, "仓库") || strings.Contains(markdown.Content, "**：x") {
		t.Errorf("空标签或空值不应渲染:\n%s", markdown.Content)
	}
}

func TestBuildCardTruncatesSummaryByRune(t *testing.T) {
	card := BuildCard(CardOptions{Summary: strings.Repeat("邮件", 2000)})
	markdown, ok := card.Body.Elements[0].(markdownElement)
	if !ok {
		t.Fatal("应包含 markdown 元素")
	}
	if !strings.HasSuffix(markdown.Content, "…") {
		t.Error("超长摘要应以省略号结尾")
	}
	if runes := []rune(markdown.Content); len(runes) > maxSummaryRunes+1 {
		t.Errorf("摘要长度 = %d, want <= %d", len(runes), maxSummaryRunes+1)
	}
}

func TestBuildCardJSONShape(t *testing.T) {
	card := BuildCard(CardOptions{Title: "t", LinkURL: "https://example.com"})
	data, err := json.Marshal(card)
	if err != nil {
		t.Fatalf("Marshal() 错误: %v", err)
	}
	var raw struct {
		Schema string `json:"schema"`
		Header struct {
			Title struct {
				Tag     string `json:"tag"`
				Content string `json:"content"`
			} `json:"title"`
			Template string `json:"template"`
		} `json:"header"`
		Body struct {
			Elements []map[string]any `json:"elements"`
		} `json:"body"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal() 错误: %v", err)
	}
	if raw.Schema != "2.0" || raw.Header.Title.Tag != "plain_text" || raw.Header.Template != TemplateBlue {
		t.Errorf("卡片 JSON 结构异常: %s", data)
	}
	if len(raw.Body.Elements) != 1 || raw.Body.Elements[0]["tag"] != "action" {
		t.Errorf("按钮元素异常: %s", data)
	}
}
