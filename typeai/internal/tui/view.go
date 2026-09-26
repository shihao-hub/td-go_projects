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

func (m *model) View() string {
	if !m.ready {
		return "终端尺寸至少需要 20x8"
	}

	errorBar := ""
	if m.operationErr != nil {
		errorBar = errorStyle.Render("error: " + errorSummary(m.operationErr))
	}
	return lipgloss.JoinVertical(
		lipgloss.Left,
		m.viewport.View(),
		m.input.View(),
		m.statusView(),
		dimStyle.Render("Enter 发送 · Ctrl+J 换行 · Ctrl+T thinking · PgUp/PgDn 滚动 · Ctrl+C 退出"),
		errorBar,
	)
}

func (m *model) statusView() string {
	reasoningChars := 0
	if m.active != nil {
		reasoningChars = m.active.ReasoningChars
	} else if len(m.messages) > 0 {
		reasoningChars = m.messages[len(m.messages)-1].ReasoningChars
	}
	return fmt.Sprintf(
		"%s · %s · %s · thinking %d chars",
		m.chat.Model(),
		truncateRunes(m.sessionLabel(), max(8, m.viewport.Width/3)),
		m.currentStatus(),
		reasoningChars,
	)
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
		out.WriteString(wrapPlainText(message.Content, width))
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
		out.WriteString(m.renderAssistantMarkdown(fmt.Sprintf("message-%04d", index), message.Content, width))
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
		out.WriteString(m.renderAssistantMarkdown(m.active.ID, m.active.Answer.String(), width))
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
