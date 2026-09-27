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
	"typeai/internal/session"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

const (
	maxReasoningWindow  = 64 * 1024
	contentFoldRunes    = 2000
	contentFoldLines    = 40
	contentPreviewLines = 6
	tabBarHeight        = 1
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
	staged           []service.Image
	captureClipboard func() (service.Image, error)
	clipboardBusy    bool

	// 分支运行时状态
	branches       map[string]*branchState
	branchOrder    []string
	activeBranchID string

	// 当前激活分支的投影状态（便于 view 和事件处理直接访问）
	messages []uiMessage
	active   *activeTurn
	running  bool
	status   string
	cancel   context.CancelFunc

	renderer *markdownRenderer
	cache    *markdownCache
	style    string

	width               int
	height              int
	ready               bool
	quitting            bool
	follow              bool
	contentOpen         bool
	operationErr        error
	inputErr            error
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

	// 测试与无服务场景允许 nil chat，等价于空 session
	var sess session.Session
	if chat != nil {
		sess = chat.Session()
	}
	branches := make(map[string]*branchState, len(sess.Branches))
	branchOrder := make([]string, 0, len(sess.Branches))
	for _, b := range sess.Branches {
		bs := newBranchState(b.ID, b.ParentID, b.ForkMessageIndex, sessionMessagesToUIMessages(b.Messages), b.CreatedAt)
		branches[b.ID] = bs
		branchOrder = append(branchOrder, b.ID)
	}

	activeID := sess.ActiveBranchID
	if activeID == "" || branches[activeID] == nil {
		activeID = sess.RootBranchID
	}
	if activeID == "" || branches[activeID] == nil {
		activeID = session.DefaultRootBranchID
	}
	if branches[activeID] == nil {
		branches[activeID] = newBranchState(activeID, "", 0, nil, time.Now())
		branchOrder = append(branchOrder, activeID)
	}

	initialMessages := resolveBranchUIMessages(activeID, branches)

	return &model{
		chat:             chat,
		now:              time.Now,
		captureClipboard: service.CaptureClipboardImage,
		input:            input,
		viewport:         viewport.New(width, 10),
		branches:         branches,
		branchOrder:      branchOrder,
		activeBranchID:   activeID,
		messages:         initialMessages,
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
	return tea.Batch(textarea.Blink, m.queueMarkdownRender(m.now(), true))
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var commands []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.resize(msg.Width, msg.Height)

	case streamDeltaMsg:
		m.applyDelta(msg)
		if msg.BranchID == m.activeBranchID {
			commands = append(commands, m.queueViewportRefresh(m.now()))
		}

	case streamResultMsg:
		cmd := m.finishStream(msg.BranchID, msg.TurnID, msg.Err)
		if cmd != nil {
			commands = append(commands, cmd)
		}

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
	case "ctrl+shift+f", "ctrl+F", "alt+f", "ctrl+b":
		m.settlePendingEnter()
		return m.fork(), false
	case "ctrl+left", "ctrl+up", "ctrl+pgup", "ctrl+pageup", "alt+up", "alt+left":
		cmd := m.prevBranch()
		return cmd, false
	case "ctrl+right", "ctrl+down", "ctrl+pgdown", "ctrl+pagedown", "alt+down", "alt+right":
		cmd := m.nextBranch()
		return cmd, false
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
	anyRunning := false
	for _, b := range m.branches {
		if b.Running {
			anyRunning = true
			if b.Cancel != nil {
				b.Cancel()
			}
		}
	}
	if anyRunning {
		m.quitting = true
		m.status = "cancelling"
		return nil
	}
	return tea.Quit
}

func (m *model) switchBranch(targetID string) (tea.Cmd, error) {
	if targetID == m.activeBranchID {
		return nil, nil
	}
	target, ok := m.branches[targetID]
	if !ok {
		return nil, fmt.Errorf("分支 %s 不存在", targetID)
	}

	// 1. 保存当前激活分支的状态与草稿
	if current, ok := m.branches[m.activeBranchID]; ok {
		current.DraftInput = m.input.Value()
		current.Staged = m.staged
		current.Active = m.active
		current.Running = m.running
		current.Status = m.status
		current.OperationErr = m.operationErr
		current.Cancel = m.cancel
	}

	// 2. 切换激活 ID
	m.activeBranchID = targetID

	// 3. 恢复目标分支的状态与草稿
	m.messages = resolveBranchUIMessages(targetID, m.branches)
	m.active = target.Active
	m.running = target.Running
	m.status = target.Status
	m.operationErr = target.OperationErr
	m.cancel = target.Cancel
	m.input.SetValue(target.DraftInput)
	m.input.CursorEnd()
	m.staged = target.Staged

	// 4. 同步至 service，如已持久化则原子保存 active_branch_id
	if err := m.chat.SetActiveBranch(targetID); err != nil {
		m.operationErr = err
	}

	// 5. 刷新视图与 Markdown 缓存
	m.syncInputLayout()
	m.markRenderDirty()
	m.refreshViewport()
	return m.queueMarkdownRender(m.now(), true), nil
}

func (m *model) prevBranch() tea.Cmd {
	if len(m.branchOrder) <= 1 {
		return nil
	}
	idx := -1
	for i, id := range m.branchOrder {
		if id == m.activeBranchID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil
	}
	prevIdx := (idx - 1 + len(m.branchOrder)) % len(m.branchOrder)
	cmd, _ := m.switchBranch(m.branchOrder[prevIdx])
	return cmd
}

func (m *model) nextBranch() tea.Cmd {
	if len(m.branchOrder) <= 1 {
		return nil
	}
	idx := -1
	for i, id := range m.branchOrder {
		if id == m.activeBranchID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil
	}
	nextIdx := (idx + 1) % len(m.branchOrder)
	cmd, _ := m.switchBranch(m.branchOrder[nextIdx])
	return cmd
}

func (m *model) fork() tea.Cmd {
	if m.running {
		m.operationErr = fmt.Errorf("无法创建分支: AI 正在流式输出中")
		m.status = "error"
		m.refreshViewport()
		return nil
	}

	resolvedMsgs := resolveBranchUIMessages(m.activeBranchID, m.branches)
	lastAIIdx := -1
	for i := len(resolvedMsgs) - 1; i >= 0; i-- {
		if resolvedMsgs[i].Role == "assistant" && !resolvedMsgs[i].Failed && resolvedMsgs[i].Content != "" {
			lastAIIdx = i
			break
		}
	}

	if lastAIIdx < 0 {
		m.operationErr = fmt.Errorf("无法创建分支: 当前分支没有已完成的 AI 消息")
		m.status = "error"
		m.refreshViewport()
		return nil
	}

	forkMessageIndex := lastAIIdx + 1

	now := m.now()
	sessBranches := m.chat.Session().Branches
	newBranch, err := session.CreateBranch(m.activeBranchID, forkMessageIndex, sessBranches, now)
	if err != nil {
		m.operationErr = err
		m.status = "error"
		m.refreshViewport()
		return nil
	}

	if err := m.chat.AddBranch(newBranch); err != nil {
		m.operationErr = err
		m.status = "error"
		m.refreshViewport()
		return nil
	}

	// 继承当前输入框的草稿；若输入框为空，则尝试从系统剪贴板获取复制的选区文本
	draft := strings.TrimSpace(m.input.Value())
	if draft == "" {
		if clip, err := clipboard.ReadAll(); err == nil {
			clip = strings.TrimSpace(clip)
			if len(clip) > 0 && len(clip) < 50000 {
				draft = clip
			}
		}
	}

	bs := newBranchState(newBranch.ID, newBranch.ParentID, newBranch.ForkMessageIndex, nil, now)
	bs.DraftInput = draft
	m.branches[newBranch.ID] = bs
	m.branchOrder = append(m.branchOrder, newBranch.ID)

	cmd, err := m.switchBranch(newBranch.ID)
	if err != nil {
		m.operationErr = err
		m.status = "error"
		m.refreshViewport()
		return nil
	}

	m.status = "ready"
	m.operationErr = nil
	m.refreshViewport()
	return cmd
}

func (m *model) deleteBranch(targetID string) tea.Cmd {
	targetID = strings.TrimSpace(targetID)
	if targetID == "" {
		targetID = m.activeBranchID
	}
	if targetID == m.chat.Session().RootBranchID {
		m.operationErr = fmt.Errorf("无法删除根分支 %s", targetID)
		m.status = "error"
		m.refreshViewport()
		return nil
	}
	b, ok := m.branches[targetID]
	if !ok {
		m.operationErr = fmt.Errorf("分支 %s 不存在", targetID)
		m.status = "error"
		m.refreshViewport()
		return nil
	}
	if b.Running {
		m.operationErr = fmt.Errorf("无法删除分支 %s: AI 正在流式输出中", targetID)
		m.status = "error"
		m.refreshViewport()
		return nil
	}

	if err := m.chat.DeleteBranch(targetID); err != nil {
		m.operationErr = err
		m.status = "error"
		m.refreshViewport()
		return nil
	}

	delete(m.branches, targetID)
	newOrder := make([]string, 0, len(m.branchOrder)-1)
	for _, id := range m.branchOrder {
		if id != targetID {
			newOrder = append(newOrder, id)
		}
	}
	m.branchOrder = newOrder

	if m.activeBranchID == targetID {
		newActive := m.chat.Session().ActiveBranchID
		if newActive == "" || m.branches[newActive] == nil {
			newActive = m.chat.Session().RootBranchID
		}
		m.activeBranchID = newActive
		m.messages = resolveBranchUIMessages(m.activeBranchID, m.branches)
		target := m.branches[m.activeBranchID]
		if target != nil {
			m.active = target.Active
			m.running = target.Running
			m.status = target.Status
			m.operationErr = target.OperationErr
			m.cancel = target.Cancel
			m.input.SetValue(target.DraftInput)
			m.input.CursorEnd()
			m.staged = target.Staged
		}
		m.syncInputLayout()
		m.markRenderDirty()
		m.refreshViewport()
		return m.queueMarkdownRender(m.now(), true)
	}

	m.status = "ready"
	m.operationErr = nil
	m.refreshViewport()
	return nil
}

func (m *model) renameBranch(oldID, newName string) tea.Cmd {
	oldID = strings.TrimSpace(oldID)
	newName = strings.TrimSpace(newName)
	if newName == "" {
		newName = oldID
		oldID = m.activeBranchID
	}
	if oldID == "" || newName == "" {
		m.operationErr = fmt.Errorf("用法: /branch rename <新分支名> 或 /branch rename <旧分支名> <新分支名>")
		m.status = "error"
		m.refreshViewport()
		return nil
	}
	b, ok := m.branches[oldID]
	if !ok {
		m.operationErr = fmt.Errorf("分支 %s 不存在", oldID)
		m.status = "error"
		m.refreshViewport()
		return nil
	}
	if b.Running {
		m.operationErr = fmt.Errorf("无法重命名分支 %s: AI 正在流式输出中", oldID)
		m.status = "error"
		m.refreshViewport()
		return nil
	}

	if err := m.chat.RenameBranch(oldID, newName); err != nil {
		m.operationErr = err
		m.status = "error"
		m.refreshViewport()
		return nil
	}

	targetState := m.branches[oldID]
	delete(m.branches, oldID)
	targetState.ID = newName
	m.branches[newName] = targetState

	for _, b := range m.branches {
		if b.ParentID == oldID {
			b.ParentID = newName
		}
	}

	for i, id := range m.branchOrder {
		if id == oldID {
			m.branchOrder[i] = newName
			break
		}
	}

	if m.activeBranchID == oldID {
		m.activeBranchID = newName
	}

	m.status = "ready"
	m.operationErr = nil
	m.refreshViewport()
	return nil
}

func (m *model) resume(sessionID string) tea.Cmd {
	// 任意分支（含后台 Tab）存在流式请求时都必须拒绝，防止替换分支树后丢失在途事件
	for id, b := range m.branches {
		if b.Running {
			m.operationErr = fmt.Errorf("无法恢复会话: 分支 %s 请求正在运行中", id)
			m.status = "error"
			m.refreshViewport()
			return nil
		}
		if len(b.Staged) > 0 {
			m.operationErr = fmt.Errorf("无法恢复会话: 分支 %s 存在暂存的图片，请先 /detach", id)
			m.status = "error"
			m.refreshViewport()
			return nil
		}
	}

	sessionID = strings.TrimSpace(sessionID)
	if !session.IsValidSessionID(sessionID) {
		m.operationErr = fmt.Errorf("非法 session ID: 必须为 8 位小写十六进制字符")
		m.status = "error"
		m.refreshViewport()
		return nil
	}

	loadedSess, err := m.chat.Resume(sessionID)
	if err != nil {
		m.operationErr = err
		m.status = "error"
		m.refreshViewport()
		return nil
	}

	newBranches := make(map[string]*branchState, len(loadedSess.Branches))
	newBranchOrder := make([]string, 0, len(loadedSess.Branches))
	for _, b := range loadedSess.Branches {
		newBranches[b.ID] = newBranchState(b.ID, b.ParentID, b.ForkMessageIndex, sessionMessagesToUIMessages(b.Messages), b.CreatedAt)
		newBranchOrder = append(newBranchOrder, b.ID)
	}

	m.branches = newBranches
	m.branchOrder = newBranchOrder

	activeID := loadedSess.ActiveBranchID
	if activeID == "" || m.branches[activeID] == nil {
		activeID = loadedSess.RootBranchID
	}
	if activeID == "" || m.branches[activeID] == nil {
		activeID = session.DefaultRootBranchID
	}

	m.activeBranchID = activeID
	m.messages = resolveBranchUIMessages(m.activeBranchID, m.branches)
	m.active = nil
	m.running = false
	m.status = "ready"
	m.operationErr = nil
	m.inputErr = nil
	m.input.Reset()
	m.staged = nil
	m.syncInputLayout()
	m.markRenderDirty()
	m.refreshViewport()
	return m.queueMarkdownRender(m.now(), true)
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
	if input == "/fork" {
		m.input.Reset()
		m.syncInputLayout()
		return m.fork()
	}
	if strings.HasPrefix(input, "/branch rm") || strings.HasPrefix(input, "/branch delete") || input == "/rm" || strings.HasPrefix(input, "/rm ") {
		var arg string
		if strings.HasPrefix(input, "/branch rm") {
			arg = strings.TrimSpace(strings.TrimPrefix(input, "/branch rm"))
		} else if strings.HasPrefix(input, "/branch delete") {
			arg = strings.TrimSpace(strings.TrimPrefix(input, "/branch delete"))
		} else {
			arg = strings.TrimSpace(strings.TrimPrefix(input, "/rm"))
		}
		m.input.Reset()
		m.syncInputLayout()
		return m.deleteBranch(arg)
	}
	if strings.HasPrefix(input, "/branch rename") || input == "/rename" || strings.HasPrefix(input, "/rename ") {
		var arg string
		if strings.HasPrefix(input, "/branch rename") {
			arg = strings.TrimSpace(strings.TrimPrefix(input, "/branch rename"))
		} else {
			arg = strings.TrimSpace(strings.TrimPrefix(input, "/rename"))
		}
		m.input.Reset()
		m.syncInputLayout()
		parts := strings.Fields(arg)
		if len(parts) == 1 {
			return m.renameBranch(m.activeBranchID, parts[0])
		} else if len(parts) == 2 {
			return m.renameBranch(parts[0], parts[1])
		}
		m.operationErr = fmt.Errorf("用法: /branch rename <新分支名> 或 /branch rename <旧分支名> <新分支名>")
		m.status = "error"
		m.refreshViewport()
		return nil
	}
	if input == "/branch" || strings.HasPrefix(input, "/branch ") {
		arg := strings.TrimSpace(strings.TrimPrefix(input, "/branch"))
		m.input.Reset()
		m.syncInputLayout()
		if arg == "" {
			m.operationErr = fmt.Errorf("用法: /branch <分支ID>")
			m.status = "error"
			m.refreshViewport()
			return nil
		}
		cmd, err := m.switchBranch(arg)
		if err != nil {
			m.operationErr = err
			m.status = "error"
			m.refreshViewport()
			return nil
		}
		return cmd
	}
	if input == "/resume" || strings.HasPrefix(input, "/resume ") {
		arg := strings.TrimSpace(strings.TrimPrefix(input, "/resume"))
		m.input.Reset()
		m.syncInputLayout()
		if arg == "" {
			m.operationErr = fmt.Errorf("用法: /resume <session-id>")
			m.status = "error"
			m.refreshViewport()
			return nil
		}
		return m.resume(arg)
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
	m.contentOpen = true // 默认不折叠

	branchID := m.activeBranchID
	b := m.branches[branchID]
	if b == nil {
		m.operationErr = fmt.Errorf("当前分支不存在: %s", branchID)
		m.status = "error"
		m.refreshViewport()
		return nil
	}

	turnID := fmt.Sprintf("turn-%04d", len(m.messages)/2+1)
	userMsg := uiMessage{
		Role:    "user",
		Content: sanitizePlainText(input),
		Images:  images,
	}
	b.Messages = append(b.Messages, userMsg)
	m.messages = append(m.messages, userMsg)

	b.Active = &activeTurn{
		ID:        turnID,
		StartedAt: m.now(),
		Images:    images,
	}
	m.active = b.Active

	m.staged = nil
	b.Staged = nil
	m.running = true
	b.Running = true
	m.quitting = false
	m.follow = true
	m.status = "running"
	b.Status = "running"
	m.operationErr = nil
	b.OperationErr = nil
	m.markRenderDirty()
	m.refreshViewport()

	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	b.Cancel = cancel
	if m.sender == nil {
		return nil
	}
	return tea.Batch(
		startStreamWithImages(m.sender, m.chat, ctx, branchID, turnID, input, images),
		scheduleElapsedTick(),
	)
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
	b, ok := m.branches[msg.BranchID]
	if !ok || b.Active == nil || b.Active.ID != msg.TurnID {
		return
	}
	switch msg.Kind {
	case llm.DeltaReasoning:
		b.Active.Reasoning.WriteString(msg.Text)
		b.Active.ReasoningChars += len([]rune(msg.Text))
		windowed := trimLeadingRunes(b.Active.Reasoning.String(), maxReasoningWindow)
		b.Active.Reasoning.Reset()
		b.Active.Reasoning.WriteString(windowed)
	case llm.DeltaAnswer:
		b.Active.Answer.WriteString(msg.Text)
	}

	if msg.BranchID == m.activeBranchID {
		m.active = b.Active
		m.markViewportDirty()
	}
}

func (m *model) finishStream(branchID, turnID string, err error) tea.Cmd {
	b, ok := m.branches[branchID]
	if !ok {
		return nil
	}
	if b.Cancel != nil {
		b.Cancel()
		b.Cancel = nil
	}

	turn := b.Active
	b.Running = false
	b.Status = "error"
	if err == nil {
		b.Status = "saved"
	}
	if turn != nil {
		turn.Failed = err != nil
		assistantMsg := uiMessage{
			Role:           "assistant",
			Content:        turn.Answer.String(),
			Reasoning:      turn.Reasoning.String(),
			ReasoningChars: turn.ReasoningChars,
			Failed:         turn.Failed,
			Images:         turn.Images,
		}
		b.Messages = append(b.Messages, assistantMsg)
		b.Active = nil
	}

	var operation *service.OperationError
	if errors.As(err, &operation) {
		b.OperationErr = operation
	} else if err != nil {
		b.OperationErr = err
	} else {
		b.OperationErr = nil
	}

	if branchID == m.activeBranchID {
		m.running = b.Running
		m.status = b.Status
		m.active = nil
		m.operationErr = b.OperationErr
		m.messages = resolveBranchUIMessages(branchID, m.branches)
		m.markRenderDirty()
		m.markViewportDirty()
		m.refreshViewport()
		if m.quitting {
			return tea.Quit
		}
		return m.queueMarkdownRender(m.now(), true)
	}

	return nil
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
	viewportHeight := max(1, m.height-desiredHeight-inputChromeHeight-tabBarHeight)
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
