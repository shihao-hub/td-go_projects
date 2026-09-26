package tui

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"typeai/internal/llm"
	"typeai/internal/service"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

const maxReasoningWindow = 64 * 1024

type uiMessage struct {
	Role           string
	Content        string
	Reasoning      string
	ReasoningChars int
	ReasoningOpen  bool
	Failed         bool
}

type activeTurn struct {
	ID             string
	Answer         strings.Builder
	Reasoning      strings.Builder
	ReasoningChars int
	ReasoningOpen  bool
	StartedAt      time.Time
	Failed         bool
}

type model struct {
	chat     *service.Chat
	sender   sender
	now      func() time.Time
	input    textarea.Model
	viewport viewport.Model
	messages []uiMessage
	active   *activeTurn

	renderer *markdownRenderer
	cache    *markdownCache
	style    string

	width               int
	height              int
	ready               bool
	running             bool
	quitting            bool
	follow              bool
	status              string
	operationErr        error
	cancel              context.CancelFunc
	renderGeneration    uint64
	rendering           bool
	renderDirty         bool
	renderTickScheduled bool
	nextRenderAt        time.Time
}

func newModel(chat *service.Chat, renderer *markdownRenderer, color bool, width, height int) *model {
	input := textarea.New()
	input.Placeholder = "输入消息，Enter 发送"
	input.Prompt = "> "
	input.ShowLineNumbers = false
	input.Focus()

	return &model{
		chat:     chat,
		now:      time.Now,
		input:    input,
		viewport: viewport.New(width, 10),
		renderer: renderer,
		cache:    newMarkdownCache(),
		style:    styleName(color),
		width:    width,
		height:   height,
		status:   "ready",
		follow:   true,
	}
}

func (m *model) Init() tea.Cmd {
	return textarea.Blink
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var commands []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.resize(msg.Width, msg.Height)

	case streamDeltaMsg:
		m.applyDelta(msg)
		commands = append(commands, m.queueMarkdownRender(m.now(), false))

	case streamResultMsg:
		commands = append(commands, m.finishStream(msg.Err))

	case markdownRenderMsg:
		commands = append(commands, m.applyRenderedMarkdown(msg))

	case renderTickMsg:
		m.renderTickScheduled = false
		commands = append(commands, m.queueMarkdownRender(m.now(), false))

	case elapsedTickMsg:
		if m.running {
			m.status = "running"
			commands = append(commands, scheduleElapsedTick())
		}

	case tea.KeyMsg:
		cmd, quit := m.handleKey(msg)
		if quit {
			return m, cmd
		}
		if cmd != nil {
			commands = append(commands, cmd)
		}
	}

	if !m.ready {
		m.refreshViewport()
	}
	return m, tea.Batch(commands...)
}

func (m *model) handleKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	switch msg.String() {
	case "ctrl+c":
		return m.requestQuit(), true
	case "ctrl+t":
		m.toggleReasoning()
		m.refreshViewport()
		return nil, false
	case "enter":
		cmd := m.submit()
		m.refreshViewport()
		return cmd, false
	case "ctrl+j":
		m.input.InsertString("\n")
		return nil, false
	case "up":
		m.viewport.LineUp(1)
		m.follow = m.viewport.AtBottom()
		return nil, false
	case "down":
		m.viewport.LineDown(1)
		m.follow = m.viewport.AtBottom()
		return nil, false
	case "pgup":
		m.viewport.PageUp()
		m.follow = false
		return nil, false
	case "pgdown":
		m.viewport.PageDown()
		m.follow = m.viewport.AtBottom()
		return nil, false
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return cmd, false
}

func (m *model) requestQuit() tea.Cmd {
	if m.running {
		if m.cancel != nil {
			m.cancel()
		}
		m.quitting = true
		m.status = "cancelling"
		return nil
	}
	return tea.Quit
}

func (m *model) submit() tea.Cmd {
	if m.running {
		return nil
	}

	input := strings.TrimSpace(m.input.Value())
	if input == "" {
		return nil
	}
	if input == "/exit" || input == "/quit" {
		return m.requestQuit()
	}
	m.input.Reset()

	id := fmt.Sprintf("turn-%04d", len(m.messages)/2+1)
	m.messages = append(m.messages, uiMessage{
		Role:    "user",
		Content: sanitizePlainText(input),
	})
	m.active = &activeTurn{
		ID:        id,
		StartedAt: m.now(),
	}
	m.running = true
	m.quitting = false
	m.follow = true
	m.status = "running"
	m.operationErr = nil
	m.markRenderDirty()

	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	if m.sender == nil {
		return nil
	}
	return tea.Batch(startStream(m.sender, m.chat, ctx, input), scheduleElapsedTick())
}

func (m *model) applyDelta(msg streamDeltaMsg) {
	if m.active == nil {
		return
	}
	switch msg.Kind {
	case llm.DeltaReasoning:
		m.active.Reasoning.WriteString(msg.Text)
		m.active.ReasoningChars += len([]rune(msg.Text))
		windowed := trimLeadingRunes(m.active.Reasoning.String(), maxReasoningWindow)
		m.active.Reasoning.Reset()
		m.active.Reasoning.WriteString(windowed)
	case llm.DeltaAnswer:
		m.active.Answer.WriteString(msg.Text)
		m.markRenderDirty()
	}
	m.refreshViewport()
}

func (m *model) finishStream(err error) tea.Cmd {
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}

	turn := m.active
	m.running = false
	m.status = "error"
	if err == nil {
		m.status = "saved"
	}
	if turn != nil {
		turn.Failed = err != nil
		m.messages = append(m.messages, uiMessage{
			Role:           "assistant",
			Content:        turn.Answer.String(),
			Reasoning:      turn.Reasoning.String(),
			ReasoningChars: turn.ReasoningChars,
			Failed:         turn.Failed,
		})
		m.active = nil
	}

	var operation *service.OperationError
	if errors.As(err, &operation) {
		m.operationErr = operation
	} else if err != nil {
		m.operationErr = err
	} else {
		m.operationErr = nil
	}

	m.follow = true
	m.markRenderDirty()
	m.refreshViewport()
	if m.quitting {
		return tea.Quit
	}
	return m.queueMarkdownRender(m.now(), true)
}

func (m *model) toggleReasoning() {
	if m.active != nil {
		m.active.ReasoningOpen = !m.active.ReasoningOpen
		return
	}
	for i := len(m.messages) - 1; i >= 0; i-- {
		if m.messages[i].Role == "assistant" {
			m.messages[i].ReasoningOpen = !m.messages[i].ReasoningOpen
			return
		}
	}
}

func (m *model) resize(width, height int) {
	m.width = max(width, 1)
	m.height = max(height, 1)
	m.ready = m.width >= 20 && m.height >= 8

	inputHeight := min(6, max(3, m.height/4))
	chromeHeight := 3
	viewportHeight := max(1, m.height-inputHeight-chromeHeight)
	contentWidth := max(1, m.width-2)

	m.input.SetWidth(contentWidth)
	m.input.SetHeight(inputHeight)
	m.viewport.Width = contentWidth
	m.viewport.Height = viewportHeight
	m.refreshViewport()
	m.markRenderDirty()
}

func (m *model) refreshViewport() {
	if m.viewport.Height <= 0 {
		m.viewport.Height = 1
	}
	follow := m.follow || m.viewport.AtBottom()
	m.viewport.SetContent(m.transcript())
	if follow {
		m.viewport.GotoBottom()
	}
}

func (m *model) currentStatus() string {
	if m.quitting {
		return "cancelling"
	}
	if m.running {
		return "running"
	}
	return m.status
}

func (m *model) sessionLabel() string {
	return filepath.Base(m.chat.Path())
}

func (m *model) renderWidth() int {
	return clampRenderWidth(m.viewport.Width)
}

func (m *model) renderTarget() (string, string, bool) {
	renderWidth := m.renderWidth()
	if m.active != nil {
		id := m.active.ID
		source := m.active.Answer.String()
		key := cacheKeyFor(id, source, renderWidth, m.style)
		if _, cached := m.cache.get(key); !cached {
			return id, source, true
		}
	}
	for i := len(m.messages) - 1; i >= 0; i-- {
		message := m.messages[i]
		if message.Role != "assistant" || message.Content == "" {
			continue
		}
		id := fmt.Sprintf("message-%04d", i)
		key := cacheKeyFor(id, message.Content, renderWidth, m.style)
		if _, cached := m.cache.get(key); !cached {
			return id, message.Content, true
		}
	}
	return "", "", false
}

func trimLeadingRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[len(runes)-limit:])
}

func sanitizePlainText(value string) string {
	var out strings.Builder
	for _, r := range value {
		if r == '\n' || r == '\t' || (r >= 0x20 && r != 0x7f) {
			out.WriteRune(r)
		}
	}
	return out.String()
}

func scheduleElapsedTick() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg {
		return elapsedTickMsg{}
	})
}
