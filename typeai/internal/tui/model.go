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

const (
	maxReasoningWindow  = 64 * 1024
	contentFoldRunes    = 2000
	contentFoldLines    = 40
	contentPreviewLines = 6
	inputFrameHeight    = 2
	inputChromeHeight   = 3 + inputFrameHeight
	minInputHeight      = 1
	maxInputHeight      = 6
	enterSubmitInterval = 75 * time.Millisecond
)

type uiMessage struct {
	Role           string
	Content        string
	Reasoning      string
	ReasoningChars int
	ReasoningOpen  bool
	Failed         bool
	Images         []service.Image
}

type activeTurn struct {
	ID             string
	Answer         strings.Builder
	Reasoning      strings.Builder
	ReasoningChars int
	ReasoningOpen  bool
	StartedAt      time.Time
	Failed         bool
	Images         []service.Image
}

type model struct {
	chat             *service.Chat
	sender           sender
	now              func() time.Time
	input            textarea.Model
	viewport         viewport.Model
	messages         []uiMessage
	active           *activeTurn
	staged           []service.Image
	captureClipboard func() (service.Image, error)
	clipboardBusy    bool

	renderer *markdownRenderer
	cache    *markdownCache
	style    string

	width               int
	height              int
	ready               bool
	running             bool
	quitting            bool
	follow              bool
	contentOpen         bool
	status              string
	operationErr        error
	inputErr            error
	cancel              context.CancelFunc
	renderGeneration    uint64
	rendering           bool
	renderDirty         bool
	renderTickScheduled bool
	nextRenderAt        time.Time
	viewportDirty       bool
	viewportScheduled   bool
	nextViewportAt      time.Time
	submitGeneration    uint64
	pendingSubmit       bool
}

func newModel(chat *service.Chat, renderer *markdownRenderer, color bool, width, height int) *model {
	input := textarea.New()
	input.Placeholder = ""
	input.Prompt = ""
	input.ShowLineNumbers = false
	input.FocusedStyle = textarea.Style{}
	input.BlurredStyle = textarea.Style{}
	input.Focus()

	return &model{
		chat:             chat,
		now:              time.Now,
		captureClipboard: service.CaptureClipboardImage,
		input:            input,
		viewport:         viewport.New(width, 10),
		renderer:         renderer,
		cache:            newMarkdownCache(),
		style:            styleName(color),
		width:            width,
		height:           height,
		status:           "ready",
		follow:           true,
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
		commands = append(commands, m.queueViewportRefresh(m.now()))

	case streamResultMsg:
		commands = append(commands, m.finishStream(msg.Err))

	case markdownRenderMsg:
		commands = append(commands, m.applyRenderedMarkdown(msg))

	case renderTickMsg:
		m.renderTickScheduled = false
		commands = append(commands, m.queueMarkdownRender(m.now(), false))

	case submitTickMsg:
		commands = append(commands, m.handleSubmitTick(msg))

	case viewportTickMsg:
		m.viewportScheduled = false
		if m.viewportDirty {
			m.viewportDirty = false
			m.refreshViewport()
			m.nextViewportAt = m.now().Add(viewportRefreshInterval)
		}

	case elapsedTickMsg:
		if m.running {
			m.status = "running"
			commands = append(commands, scheduleElapsedTick())
		}

	case clipboardImageMsg:
		m.clipboardBusy = false
		m.applyClipboardImage(msg)

	case tea.KeyMsg:
		cmd, quit := m.handleKey(msg)
		if quit {
			return m, cmd
		}
		if cmd != nil {
			commands = append(commands, cmd)
		}
	}

	// textarea's clipboard Paste command returns an unexported message type,
	// so forward non-key messages for the widget to consume itself.
	if _, isKey := msg.(tea.KeyMsg); !isKey {
		var inputCmd tea.Cmd
		m.input, inputCmd = m.input.Update(msg)
		m.inputErr = m.input.Err
		if inputCmd != nil {
			commands = append(commands, inputCmd)
		}
	}

	m.syncInputLayout()
	if !m.ready {
		m.refreshViewport()
	}
	return m, tea.Batch(commands...)
}

func (m *model) handleKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	if msg.Paste {
		m.settlePendingEnter()
		text := strings.ReplaceAll(string(msg.Runes), "\r\n", "\n")
		text = strings.ReplaceAll(text, "\r", "\n")
		m.input.InsertString(text)
		m.syncInputLayout()
		m.refreshViewport()
		return nil, false
	}
	switch msg.String() {
	case "ctrl+c":
		return m.requestQuit(), true
	case "ctrl+t":
		m.toggleReasoning()
		m.refreshViewport()
		return nil, false
	case "ctrl+e":
		m.toggleContentFold()
		m.refreshViewport()
		return nil, false
	case "enter":
		m.settlePendingEnter()
		cmd := m.scheduleSubmit()
		m.refreshViewport()
		return cmd, false
	case "ctrl+j":
		m.settlePendingEnter()
		m.input.InsertString("\n")
		m.syncInputLayout()
		return nil, false
	case "alt+v":
		return m.startClipboardCapture(), false
	case "up":
		if m.input.Line() > 0 {
			var inputCmd tea.Cmd
			m.input, inputCmd = m.input.Update(msg)
			return inputCmd, false
		}
		m.viewport.LineUp(1)
		m.follow = m.viewport.AtBottom()
		return nil, false
	case "down":
		if m.input.Line() < m.input.LineCount()-1 {
			var inputCmd tea.Cmd
			m.input, inputCmd = m.input.Update(msg)
			return inputCmd, false
		}
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
	m.settlePendingEnter()
	m.input, cmd = m.input.Update(msg)
	m.syncInputLayout()
	if msg.Paste {
		m.refreshViewport()
	}
	return cmd, false
}

func (m *model) scheduleSubmit() tea.Cmd {
	m.submitGeneration++
	m.pendingSubmit = true
	generation := m.submitGeneration
	return tea.Tick(enterSubmitInterval, func(time.Time) tea.Msg {
		return submitTickMsg{Generation: generation}
	})
}

func (m *model) handleSubmitTick(msg submitTickMsg) tea.Cmd {
	if !m.pendingSubmit || msg.Generation != m.submitGeneration {
		return nil
	}
	m.pendingSubmit = false
	return m.submit()
}

func (m *model) settlePendingEnter() {
	if !m.pendingSubmit {
		return
	}
	m.pendingSubmit = false
	m.input.InsertString("\n")
	m.syncInputLayout()
}

func (m *model) requestQuit() tea.Cmd {
	m.pendingSubmit = false
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
	if input == "/detach" {
		m.staged = nil
		m.input.Reset()
		m.syncInputLayout()
		m.operationErr = nil
		m.status = "ready"
		m.refreshViewport()
		return nil
	}
	if input == "/image" || strings.HasPrefix(input, "/image ") {
		return m.stageImage(strings.TrimSpace(strings.TrimPrefix(input, "/image")))
	}

	_, markerImages, err := service.ParseImageMarkers(input)
	if err != nil {
		m.operationErr = err
		m.status = "error"
		m.refreshViewport()
		return nil
	}
	images := make([]service.Image, 0, len(m.staged)+len(markerImages))
	images = append(images, m.staged...)
	images = append(images, markerImages...)
	if len(images) > service.MaxImagesPerMessage {
		m.operationErr = service.CodedError("invalid_image", fmt.Sprintf("每条消息最多 %d 张图片", service.MaxImagesPerMessage))
		m.status = "error"
		m.refreshViewport()
		return nil
	}
	m.input.Reset()
	m.syncInputLayout()
	m.inputErr = nil
	m.contentOpen = false

	id := fmt.Sprintf("turn-%04d", len(m.messages)/2+1)
	m.messages = append(m.messages, uiMessage{
		Role:    "user",
		Content: sanitizePlainText(input),
		Images:  images,
	})
	m.active = &activeTurn{
		ID:        id,
		StartedAt: m.now(),
		Images:    images,
	}
	m.staged = nil
	m.running = true
	m.quitting = false
	m.follow = true
	m.status = "running"
	m.operationErr = nil
	m.markRenderDirty()
	// 先把用户消息和等待状态画出来，不要等第一段 AI 流式数据。
	m.refreshViewport()

	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	if m.sender == nil {
		return nil
	}
	return tea.Batch(startStreamWithImages(m.sender, m.chat, ctx, input, m.staged), scheduleElapsedTick())
}

func (m *model) startClipboardCapture() tea.Cmd {
	if m.clipboardBusy {
		return nil
	}
	m.clipboardBusy = true
	m.status = "reading clipboard"
	m.refreshViewport()
	capture := m.captureClipboard
	return func() tea.Msg {
		image, err := capture()
		return clipboardImageMsg{Image: image, Err: err}
	}
}

func (m *model) applyClipboardImage(msg clipboardImageMsg) {
	if msg.Err != nil {
		m.operationErr = msg.Err
		m.status = "error"
		m.refreshViewport()
		return
	}
	m.operationErr = nil
	if !m.running {
		m.status = "ready"
	}
	m.input.InsertString(imageMarker(msg.Image.Path))
	m.syncInputLayout()
	m.refreshViewport()
}

func (m *model) stageImage(argument string) tea.Cmd {
	if argument == "" {
		m.operationErr = service.CodedError("invalid_image", "用法: /image <图片路径>")
		m.status = "error"
		m.refreshViewport()
		return nil
	}
	if len(m.staged) >= service.MaxImagesPerMessage {
		m.operationErr = service.CodedError("invalid_image", fmt.Sprintf("每条消息最多 %d 张图片", service.MaxImagesPerMessage))
		m.status = "error"
		m.refreshViewport()
		return nil
	}
	image, err := service.PrepareImage(argument)
	if err != nil {
		m.operationErr = err
		m.status = "error"
		m.refreshViewport()
		return nil
	}
	m.staged = append(m.staged, image)
	m.input.Reset()
	m.syncInputLayout()
	m.operationErr = nil
	m.status = "ready"
	m.refreshViewport()
	return nil
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
	}
	m.markViewportDirty()
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
			Images:         turn.Images,
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
	m.markViewportDirty()
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

func (m *model) toggleContentFold() {
	m.contentOpen = !m.contentOpen
}

func (m *model) resize(width, height int) {
	m.width = max(width, 1)
	m.height = max(height, 1)
	m.ready = m.width >= 20 && m.height >= 8

	contentWidth := max(1, m.width-2)

	m.input.SetWidth(contentWidth)
	m.viewport.Width = contentWidth
	m.syncInputLayout()
	m.markRenderDirty()
}

func (m *model) syncInputLayout() {
	maxHeight := min(maxInputHeight, max(minInputHeight, m.height/4))
	desiredHeight := min(maxHeight, max(minInputHeight, m.input.LineCount()))
	viewportHeight := max(1, m.height-desiredHeight-inputChromeHeight)
	changed := m.input.Height() != desiredHeight || m.viewport.Height != viewportHeight
	m.input.SetHeight(desiredHeight)
	m.viewport.Height = viewportHeight
	if changed {
		m.refreshViewport()
	}
}

func (m *model) refreshViewport() {
	m.viewportDirty = false
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
		return "", "", false
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
