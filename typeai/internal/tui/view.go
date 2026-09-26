package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"typeai/internal/service"

	"github.com/charmbracelet/lipgloss"
)

var userStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
var assistantStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("4"))
var dimStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
var errorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
var inputBorderStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

func (m *model) View() string {
	if !m.ready {
		return "终端尺寸至少需要 20x8"
	}

	errorBar := ""
	uiErr := m.operationErr
	prefix := "error: "
	if uiErr == nil {
		uiErr = m.inputErr
		prefix = "input: "
	}
	if uiErr != nil {
		errorBar = errorStyle.Render(prefix + errorSummary(uiErr))
	}
	sections := []string{
		m.viewport.View(),
		m.inputView(),
	}
	if staged := m.stagedView(); staged != "" {
		sections = append(sections, staged)
	}
	sections = append(sections,
		m.statusView(),
		dimStyle.Render("Enter 发送 · Ctrl+J 换行 · Ctrl+V 文本 · Alt+V 图片 · /image <路径> 添加图片 · Ctrl+T thinking · Ctrl+E 折叠 · PgUp/PgDn 滚动 · Ctrl+C 退出"),
		errorBar,
	)
	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

// inputView renders the editor like a standalone prompt area: no per-line
// prompt character, with a colored separator above and below the input.
func (m *model) inputView() string {
	frameWidth := max(1, m.width-2)
	rule := inputBorderStyle.Render(strings.Repeat("─", frameWidth))
	body := strings.TrimRight(m.input.View(), "\n")
	frame := strings.Join([]string{rule, body, rule}, "\n")
	return lipgloss.NewStyle().PaddingLeft(1).PaddingRight(1).Render(frame)
}

func (m *model) stagedView() string {
	if len(m.staged) == 0 {
		return ""
	}
	parts := make([]string, 0, len(m.staged))
	for _, image := range m.staged {
		parts = append(parts, fmt.Sprintf(
			"image ready: %s · %s · %s",
			image.FileName,
			image.MediaType,
			truncateRunes(image.Path, max(12, m.viewport.Width/2)),
		))
	}
	return dimStyle.Render(strings.Join(parts, "\n"))
}

func (m *model) statusView() string {
	reasoningChars := 0
	if m.active != nil {
		reasoningChars = m.active.ReasoningChars
	} else if len(m.messages) > 0 {
		reasoningChars = m.messages[len(m.messages)-1].ReasoningChars
	}
	status := fmt.Sprintf(
		"%s · %s · %s · thinking %d chars",
		m.chat.Model(),
		truncateRunes(m.sessionLabel(), max(8, m.viewport.Width/3)),
		m.currentStatus(),
		reasoningChars,
	)
	if len(m.staged) > 0 {
		status += fmt.Sprintf(" · images %d", len(m.staged))
	}
	return status
}

func (m *model) transcript() string {
	var out strings.Builder
	renderWidth := clampRenderWidth(m.viewport.Width)
	for i := range m.messages {
		out.WriteString(m.renderStoredMessage(i, m.messages[i], renderWidth))
		out.WriteString("\n\n")
	}
	if m.active != nil {
		out.WriteString(m.renderActiveTurn(renderWidth))
	}
	return strings.TrimRight(out.String(), "\n")
}

func (m *model) renderStoredMessage(index int, message uiMessage, width int) string {
	var out strings.Builder
	if message.Role == "user" {
		out.WriteString(userStyle.Render("You"))
		out.WriteString("\n")
		if m.contentCollapsed(message.Content) {
			out.WriteString(previewText(wrapPlainText(message.Content, width)))
		} else {
			out.WriteString(wrapPlainText(message.Content, width))
		}
		out.WriteString(renderImageMetadata(message.Images, width))
		return out.String()
	}

	out.WriteString(assistantStyle.Render("AI"))
	if message.Failed {
		out.WriteString(" " + errorStyle.Render("(failed, not saved)"))
	}
	out.WriteString("\n")
	if message.Reasoning != "" {
		out.WriteString(m.renderReasoning(message.Reasoning, message.ReasoningChars, message.ReasoningOpen, time.Time{}, width))
		out.WriteString("\n")
	}
	if message.Content != "" {
		rendered := m.renderAssistantMarkdown(fmt.Sprintf("message-%04d", index), message.Content, width)
		if m.contentCollapsed(message.Content) {
			out.WriteString(previewText(rendered))
		} else {
			out.WriteString(rendered)
		}
	}
	if message.Failed && message.Content == "" {
		out.WriteString(dimStyle.Render("没有收到完整回答"))
	}
	return out.String()
}

func (m *model) renderActiveTurn(width int) string {
	var out strings.Builder
	out.WriteString(assistantStyle.Render("AI"))
	if m.active.Failed {
		out.WriteString(" " + errorStyle.Render("(failed, not saved)"))
	}
	out.WriteString("\n")
	if len(m.active.Images) > 0 {
		out.WriteString(renderImageMetadata(m.active.Images, width))
		out.WriteString("\n")
	}
	if m.active.Reasoning.Len() > 0 {
		out.WriteString(m.renderReasoning(
			m.active.Reasoning.String(),
			m.active.ReasoningChars,
			m.active.ReasoningOpen,
			m.active.StartedAt,
			width,
		))
		out.WriteString("\n")
	}
	if m.active.Answer.Len() > 0 {
		out.WriteString(wrapPlainText(m.active.Answer.String(), width))
	} else {
		out.WriteString(dimStyle.Render("waiting for response..."))
	}
	return out.String()
}

func (m *model) renderReasoning(value string, chars int, open bool, startedAt time.Time, width int) string {
	label := fmt.Sprintf("Thinking (%d chars)", chars)
	if !startedAt.IsZero() {
		if m.running {
			label += fmt.Sprintf(" · %ds", int(time.Since(startedAt).Seconds()))
		} else {
			label += " · done"
		}
	}
	if !open {
		return dimStyle.Render(label + " · Ctrl+T")
	}
	return dimStyle.Render(label) + "\n" + wrapPlainText(value, width)
}

func (m *model) renderAssistantMarkdown(id, source string, width int) string {
	key := cacheKeyFor(id, source, width, m.style)
	if rendered, ok := m.cache.get(key); ok {
		return rendered
	}
	return source
}

func wrapPlainText(value string, width int) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return lipgloss.NewStyle().Width(max(1, width)).Render(value)
}

func contentShouldFold(value string) bool {
	if value == "" {
		return false
	}
	return len([]rune(value)) >= contentFoldRunes || strings.Count(value, "\n")+1 >= contentFoldLines
}

func (m *model) contentCollapsed(value string) bool {
	return contentShouldFold(value) && !m.contentOpen
}

func previewText(value string) string {
	lines := strings.Split(value, "\n")
	start := 0
	for start < len(lines) && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	end := min(len(lines), start+contentPreviewLines)
	for end > start && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	preview := strings.Join(lines[start:end], "\n")
	displayLines := max(1, len(lines)-start)
	chars := len([]rune(value))
	return fmt.Sprintf(
		"%s\n%s",
		preview,
		dimStyle.Render(fmt.Sprintf("… %d chars · %d lines · Ctrl+E 展开", chars, displayLines)),
	)
}

func errorSummary(err error) string {
	var operation *service.OperationError
	if errors.As(err, &operation) {
		return fmt.Sprintf("%s: %s", operation.ErrorCode(), operation.Error())
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	return err.Error()
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	if limit <= 1 {
		return string(runes[:1])
	}
	return string(runes[:limit-1]) + "…"
}
