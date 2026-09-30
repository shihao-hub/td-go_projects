package tui

import (
	"fmt"
	"strings"

	"typeai/internal/session"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var resumePickerTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))
var resumePickerCardStyle = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(lipgloss.Color("8")).
	Padding(1, 2)
var resumePickerSelectedStyle = lipgloss.NewStyle().
	Bold(true).
	Foreground(lipgloss.Color("15")).
	Background(lipgloss.Color("4"))
var resumePickerItemStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("7"))

func (m *model) openResumePicker() tea.Cmd {
	if err := m.resumeBlocked(); err != nil {
		m.operationErr = err
		m.status = "error"
		m.refreshViewport()
		return nil
	}

	var summaries []session.SessionSummary
	if m.chat != nil {
		var err error
		summaries, err = m.chat.SessionSummaries()
		if err != nil {
			m.operationErr = err
			m.status = "error"
			m.refreshViewport()
			return nil
		}
	}

	m.resumeSummaries = append([]session.SessionSummary(nil), summaries...)
	m.resumePickerIndex = 0
	m.resumePicker = true
	m.operationErr = nil
	return nil
}

func (m *model) closeResumePicker() {
	m.resumePicker = false
	m.resumeSummaries = nil
	m.resumePickerIndex = 0
	m.operationErr = nil
}

func (m *model) moveResumePicker(delta int) {
	count := len(m.resumeSummaries)
	if !m.resumePicker || count == 0 {
		return
	}
	next := m.resumePickerIndex + delta
	m.resumePickerIndex = max(0, min(count-1, next))
}

func (m *model) selectResumePicker() (tea.Cmd, bool) {
	if !m.resumePicker {
		return nil, false
	}
	if len(m.resumeSummaries) == 0 {
		m.closeResumePicker()
		return nil, false
	}

	summary := m.resumeSummaries[m.resumePickerIndex]
	m.closeResumePicker()
	return m.resume(summary.ID), false
}

func (m *model) sessionPickerView() string {
	title := resumePickerTitleStyle.Render("Resume session")
	body := "没有已保存的会话"
	if len(m.resumeSummaries) > 0 {
		body = m.visibleResumePickerItems()
	}
	hint := dimStyle.Render("Up/Down 选择 · Enter 恢复 · Esc 取消")
	card := resumePickerCardStyle.Render(lipgloss.JoinVertical(lipgloss.Left, title, "", body, "", hint))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, card)
}

func (m *model) visibleResumePickerItems() string {
	count := len(m.resumeSummaries)
	visibleCount := max(1, (m.height-6)/4)
	start := min(count-visibleCount, m.resumePickerIndex)
	start = max(0, start)
	end := min(count, start+visibleCount)

	rows := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		rows = append(rows, resumePickerItem(m.resumeSummaries[i], i == m.resumePickerIndex))
	}
	position := dimStyle.Render(fmt.Sprintf("会话 %d/%d", m.resumePickerIndex+1, count))
	rows = append(rows, position)
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

func resumePickerItem(summary session.SessionSummary, selected bool) string {
	width := max(32, 80-6)
	updatedAt := summary.UpdatedAt.Format("2006-01-02 15:04")
	firstUser := summary.FirstUser
	if firstUser == "" {
		firstUser = "(空会话)"
	}
	lastAssistant := summary.LastAssistant
	if lastAssistant == "" {
		lastAssistant = "(未回复)"
	}

	header := fmt.Sprintf(
		"%s  %s  %s  %d msgs",
		updatedAt,
		summary.ID,
		truncateRunes(summary.Model, max(12, width/3)),
		summary.MessageCount,
	)
	branchLine := "branch: " + strings.Join(summary.Branches, ", ")
	branchLine = truncateRunes(branchLine, width)
	firstLine := "You: " + truncateRunes(firstUser, width)
	assistantLine := "AI:  " + truncateRunes(lastAssistant, width)
	body := lipgloss.JoinVertical(lipgloss.Left, header, branchLine, firstLine, assistantLine)
	if selected {
		return resumePickerSelectedStyle.Render(body)
	}
	return resumePickerItemStyle.Render(body)
}
