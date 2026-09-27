package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func newStreamFinishModel(t *testing.T) *model {
	t.Helper()

	m := newModel(nil, newMarkdownRenderer("notty"), false, 80, 24)
	m.resize(80, 24)

	content := strings.Repeat("line\n", m.viewport.Height+4)
	b := m.branches[m.activeBranchID]
	b.Messages = append(b.Messages, uiMessage{Role: "user", Content: content})
	m.messages = append([]uiMessage(nil), b.Messages...)
	m.refreshViewport()

	b.Active = &activeTurn{ID: "turn-0001"}
	b.Active.Answer.WriteString(content)
	b.Running = true
	m.active = b.Active
	m.running = true
	return m
}

func TestFinishStreamKeepsViewportAwayFromBottom(t *testing.T) {
	m := newStreamFinishModel(t)

	m.handleKey(tea.KeyMsg{Type: tea.KeyPgUp})
	if m.follow || m.viewport.AtBottom() {
		t.Fatalf("follow = %v, at bottom = %v; want viewport scrolled away", m.follow, m.viewport.AtBottom())
	}
	offset := m.viewport.YOffset

	m.finishStream(m.activeBranchID, "turn-0001", nil)
	if m.follow {
		t.Fatal("finished stream should keep follow disabled")
	}
	if m.viewport.AtBottom() {
		t.Fatal("finished stream should not force viewport to bottom")
	}
	if m.viewport.YOffset != offset {
		t.Fatalf("viewport offset = %d, want %d", m.viewport.YOffset, offset)
	}
}

func TestFinishStreamFollowsViewportAtBottom(t *testing.T) {
	m := newStreamFinishModel(t)

	m.handleKey(tea.KeyMsg{Type: tea.KeyPgUp})
	if m.follow {
		t.Fatal("PageUp should disable follow")
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyPgDown})
	if !m.follow || !m.viewport.AtBottom() {
		t.Fatalf("follow = %v, at bottom = %v; want viewport back at bottom", m.follow, m.viewport.AtBottom())
	}

	m.finishStream(m.activeBranchID, "turn-0001", nil)
	if !m.follow || !m.viewport.AtBottom() {
		t.Fatalf("follow = %v, at bottom = %v; want finished stream to follow latest content", m.follow, m.viewport.AtBottom())
	}
}
