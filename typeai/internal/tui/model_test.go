package tui

import (
	"strings"
	"testing"
	"time"

	"typeai/internal/llm"
	"typeai/internal/config"
	"typeai/internal/session"
	"typeai/internal/service"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestHandleBracketedPasteNormalizesNewlines(t *testing.T) {
	m := newModel(nil, newMarkdownRenderer("notty"), false, 80, 24)
	m.resize(80, 24)

	m.handleKey(tea.KeyMsg{
		Type:  tea.KeyRunes,
		Runes: []rune("first\r\nsecond\rthird"),
		Paste: true,
	})

	if got := m.input.Value(); got != "first\nsecond\nthird" {
		t.Fatalf("input value = %q, want %q", got, "first\nsecond\nthird")
	}
}

func TestDelayedEnterSubmitsAfterQuietWindow(t *testing.T) {
	m := newModel(nil, newMarkdownRenderer("notty"), false, 80, 24)
	m.resize(80, 24)
	m.input.SetValue("hello")

	cmd, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Enter should schedule a submit")
	}
	if !m.pendingSubmit || len(m.messages) != 0 {
		t.Fatalf("pending = %v, messages = %d; want delayed submit", m.pendingSubmit, len(m.messages))
	}

	updated, _ := m.Update(submitTickMsg{Generation: m.submitGeneration})
	submitted := updated.(*model)
	if submitted.pendingSubmit || len(submitted.messages) != 1 {
		t.Fatalf("pending = %v, messages = %d; want one submitted message", submitted.pendingSubmit, len(submitted.messages))
	}
}

func TestSubmitRefreshesUserMessageBeforeStream(t *testing.T) {
	m := newModel(nil, newMarkdownRenderer("notty"), false, 80, 24)
	m.resize(80, 24)
	m.input.SetValue("hello")

	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	updated, _ := m.Update(submitTickMsg{Generation: m.submitGeneration})
	submitted := updated.(*model)
	if got := submitted.viewport.View(); !strings.Contains(got, "hello") {
		t.Fatalf("viewport = %q, want user message before stream data", got)
	}
	if submitted.active == nil {
		t.Fatal("active turn should exist while waiting for stream")
	}
}

func TestInputAfterDelayedEnterBecomesNewline(t *testing.T) {
	m := newModel(nil, newMarkdownRenderer("notty"), false, 80, 24)
	m.resize(80, 24)
	m.input.SetValue("first")

	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})

	if got := m.input.Value(); got != "first\nx" {
		t.Fatalf("input value = %q, want %q", got, "first\nx")
	}
	if len(m.messages) != 0 {
		t.Fatalf("messages = %d, want none", len(m.messages))
	}
}

func TestArrowKeysMoveMultilineInputCursor(t *testing.T) {
	m := newModel(nil, newMarkdownRenderer("notty"), false, 80, 24)
	m.resize(80, 24)
	m.input.SetValue("first\nsecond")

	if line := m.input.Line(); line != 1 {
		t.Fatalf("initial line = %d, want 1", line)
	}
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyUp})
	if line := m.input.Line(); line != 0 {
		t.Fatalf("line after Up = %d, want 0", line)
	}
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	if line := m.input.Line(); line != 1 {
		t.Fatalf("line after Down = %d, want 1", line)
	}
}

func TestInputViewUsesFramedEditorWithoutPrompt(t *testing.T) {
	m := newModel(nil, newMarkdownRenderer("notty"), false, 80, 24)
	m.resize(80, 24)
	m.input.SetValue("hello")

	if m.input.Prompt != "" {
		t.Fatalf("input prompt = %q, want empty prompt", m.input.Prompt)
	}
	if m.input.Placeholder != "" {
		t.Fatalf("input placeholder = %q, want empty placeholder", m.input.Placeholder)
	}

	rendered := ansi.Strip(m.inputView())
	lines := strings.Split(strings.TrimRight(rendered, "\n"), "\n")
	if len(lines) != m.input.Height()+inputFrameHeight {
		t.Fatalf("rendered lines = %d, want %d", len(lines), m.input.Height()+inputFrameHeight)
	}

	expectedRule := strings.Repeat("─", m.width-2)
	if got := strings.TrimSpace(lines[0]); got != expectedRule {
		t.Fatalf("top rule = %q, want %q", got, expectedRule)
	}
	if got := strings.TrimSpace(lines[len(lines)-1]); got != expectedRule {
		t.Fatalf("bottom rule = %q, want %q", got, expectedRule)
	}
	if strings.Contains(rendered, "> ") {
		t.Fatal("input view should not contain the old prompt")
	}
}

func TestInputHeightGrowsWithCtrlJ(t *testing.T) {
	m := newModel(nil, newMarkdownRenderer("notty"), false, 80, 24)
	m.resize(80, 24)

	if got := m.input.Height(); got != minInputHeight {
		t.Fatalf("initial input height = %d, want %d", got, minInputHeight)
	}

	for i := 0; i < maxInputHeight+1; i++ {
		_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlJ})
		want := min(maxInputHeight, i+2)
		if got := m.input.Height(); got != want {
			t.Fatalf("after Ctrl+J %d input height = %d, want %d", i+1, got, want)
		}
	}

	m.input.Reset()
	m.syncInputLayout()
	if got := m.input.Height(); got != minInputHeight {
		t.Fatalf("reset input height = %d, want %d", got, minInputHeight)
	}
}

func TestStreamingTurnStaysPlainTextUntilFinished(t *testing.T) {
	m := newModel(nil, newMarkdownRenderer("notty"), false, 80, 24)
	m.resize(80, 24)
	b := m.branches[m.activeBranchID]
	b.Active = &activeTurn{ID: "turn-0001"}
	b.Running = true
	m.active = b.Active

	m.applyDelta(streamDeltaMsg{BranchID: m.activeBranchID, TurnID: "turn-0001", Kind: llm.DeltaAnswer, Text: "# Heading"})
	if _, _, ok := m.renderTarget(); ok {
		t.Fatal("active streaming turn should not be a Markdown render target")
	}

	cmd := m.finishStream(m.activeBranchID, "turn-0001", nil)
	if cmd == nil {
		t.Fatal("finished stream should schedule Markdown rendering")
	}
	if !m.rendering {
		t.Fatal("finished stream should start Markdown rendering")
	}
}

func TestViewportRefreshIsThrottled(t *testing.T) {
	m := newModel(nil, newMarkdownRenderer("notty"), false, 80, 24)
	m.resize(80, 24)
	now := time.Now()

	m.markViewportDirty()
	if cmd := m.queueViewportRefresh(now); cmd != nil {
		t.Fatal("due viewport refresh should run synchronously")
	}
	if m.viewportDirty {
		t.Fatal("due viewport refresh should clear dirty state")
	}

	m.markViewportDirty()
	cmd := m.queueViewportRefresh(now.Add(time.Millisecond))
	if cmd == nil || !m.viewportScheduled {
		t.Fatalf("scheduled = %v, cmd = %v; want delayed viewport refresh", m.viewportScheduled, cmd)
	}
}

func TestContentFoldThresholdAndPreview(t *testing.T) {
	if contentShouldFold(strings.Repeat("x", contentFoldRunes-1)) {
		t.Fatal("content just below character threshold should not fold")
	}
	if !contentShouldFold(strings.Repeat("x", contentFoldRunes)) {
		t.Fatal("content at character threshold should fold")
	}
	if !contentShouldFold(strings.Repeat("line\n", contentFoldLines)) {
		t.Fatal("content at line threshold should fold")
	}

	m := newModel(nil, newMarkdownRenderer("notty"), false, 80, 24)
	m.resize(80, 24)
	long := strings.Repeat("x", contentFoldRunes)
	if !m.contentCollapsed(long) {
		t.Fatal("long content should be collapsed by default")
	}
	m.toggleContentFold()
	if m.contentCollapsed(long) {
		t.Fatal("Ctrl+E should expand folded content")
	}

	preview := previewText(strings.Repeat("line\n", contentPreviewLines+4))
	if got := strings.Count(preview, "line") - strings.Count(preview, "lines"); got != contentPreviewLines {
		t.Fatalf("preview line count = %d, want %d", got, contentPreviewLines)
	}
	if !strings.Contains(preview, "Ctrl+E") {
		t.Fatal("preview should show fold hint")
	}
}

func TestAltVInsertsClipboardImageMarker(t *testing.T) {
	path := "C:\\Temp\\clipboard.png"
	m := newModel(nil, newMarkdownRenderer("notty"), false, 80, 24)
	m.resize(80, 24)
	m.captureClipboard = func() (service.Image, error) {
		return service.Image{Path: path, FileName: "clipboard.png", MediaType: "image/png"}, nil
	}

	cmd, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v"), Alt: true})
	if cmd == nil {
		t.Fatal("Alt+V returned nil command")
	}
	msg := cmd()
	clipboardMsg, ok := msg.(clipboardImageMsg)
	if !ok {
		t.Fatalf("message = %#v", msg)
	}
	m.applyClipboardImage(clipboardMsg)
	if got := m.input.Value(); got != imageMarker(path) {
		t.Fatalf("input = %q, want %q", got, imageMarker(path))
	}
}

func TestTitleCommand(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	chat, err := service.NewChat(config.Config{
		BaseURL: "http://localhost",
		Model:   "m",
	}, dir, now)
	if err != nil {
		t.Fatalf("NewChat() error = %v", err)
	}

	m := newModel(chat, newMarkdownRenderer("notty"), false, 80, 24)
	m.resize(80, 24)

	// Test /title <新标题>
	m.input.SetValue("/title 终端改名测试")
	cmd := m.submit()
	if cmd != nil {
		t.Fatal("expected nil command from /title")
	}
	if m.operationErr != nil {
		t.Fatalf("unexpected operationErr: %v", m.operationErr)
	}
	if chat.Session().Title != "终端改名测试" {
		t.Errorf("chat title = %q, want 终端改名测试", chat.Session().Title)
	}
	if !strings.Contains(m.status, "终端改名测试") {
		t.Errorf("m.status = %q, want containing title", m.status)
	}

	// Test /session rename <新标题>
	m.input.SetValue("/session rename 别名改名测试")
	_ = m.submit()
	if chat.Session().Title != "别名改名测试" {
		t.Errorf("chat title = %q, want 别名改名测试", chat.Session().Title)
	}
}

func TestResumePickerRendersTitle(t *testing.T) {
	summary := session.SessionSummary{
		ID:            "a1b2c3d4",
		Title:         "自定义架构设计会话",
		Model:         "deepseek-chat",
		UpdatedAt:     time.Now(),
		FirstUser:     "首轮问题",
		LastAssistant: "首轮回答",
		Branches:      []string{"main"},
	}
	rendered := resumePickerItem(summary, false)
	if !strings.Contains(rendered, "Title:  自定义架构设计会话") {
		t.Errorf("rendered resume item missing title line: %s", rendered)
	}
}
