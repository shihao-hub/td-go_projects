package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	markdownRenderInterval  = 80 * time.Millisecond
	viewportRefreshInterval = 100 * time.Millisecond
)

type markdownRenderMsg struct {
	Generation uint64
	Key        cacheKey
	Output     string
	Fallback   bool
}

type renderTickMsg struct{}

type viewportTickMsg struct{}

type submitTickMsg struct {
	Generation uint64
}

func (m *model) markRenderDirty() {
	m.renderDirty = true
}

func (m *model) markViewportDirty() {
	m.viewportDirty = true
}

func (m *model) queueViewportRefresh(now time.Time) tea.Cmd {
	if !m.viewportDirty || m.viewportScheduled {
		return nil
	}
	if m.nextViewportAt.IsZero() || !now.Before(m.nextViewportAt) {
		m.viewportDirty = false
		m.nextViewportAt = now.Add(viewportRefreshInterval)
		m.refreshViewport()
		return nil
	}
	m.viewportScheduled = true
	delay := max(time.Millisecond, time.Until(m.nextViewportAt))
	return tea.Tick(delay, func(time.Time) tea.Msg {
		return viewportTickMsg{}
	})
}

func (m *model) queueMarkdownRender(now time.Time, force bool) tea.Cmd {
	if force {
		return m.startMarkdownRender(now, true)
	}
	if !m.renderDirty || m.rendering {
		return nil
	}
	if m.nextRenderAt.IsZero() || !now.Before(m.nextRenderAt) {
		return m.startMarkdownRender(now, false)
	}
	if m.renderTickScheduled {
		return nil
	}
	m.renderTickScheduled = true
	delay := max(time.Millisecond, time.Until(m.nextRenderAt))
	return tea.Tick(delay, func(time.Time) tea.Msg {
		return renderTickMsg{}
	})
}

func (m *model) startMarkdownRender(now time.Time, force bool) tea.Cmd {
	if m.rendering {
		return nil
	}

	id, source, ok := m.renderTarget()
	if !ok || source == "" {
		return nil
	}
	key := cacheKeyFor(id, source, m.renderWidth(), m.style)
	if _, cached := m.cache.get(key); cached {
		m.renderDirty = false
		return nil
	}
	if !force && !m.nextRenderAt.IsZero() && now.Before(m.nextRenderAt) {
		return m.queueMarkdownRender(now, false)
	}

	m.rendering = true
	m.renderDirty = false
	m.renderGeneration++
	generation := m.renderGeneration
	m.nextRenderAt = now.Add(markdownRenderInterval)
	renderer := m.renderer
	return func() tea.Msg {
		output, err := renderer.Render(source, key.Width, key.Style)
		fallback := err != nil
		if fallback {
			output = source
		}
		return markdownRenderMsg{
			Generation: generation,
			Key:        key,
			Output:     output,
			Fallback:   fallback,
		}
	}
}

func (m *model) applyRenderedMarkdown(msg markdownRenderMsg) tea.Cmd {
	m.rendering = false
	m.cache.put(msg.Key, msg.Output)
	m.nextRenderAt = m.now().Add(markdownRenderInterval)
	m.refreshViewport()

	if m.renderDirty {
		return m.queueMarkdownRender(m.now(), false)
	}

	id, source, ok := m.renderTarget()
	if ok && source != "" {
		key := cacheKeyFor(id, source, m.renderWidth(), m.style)
		if _, cached := m.cache.get(key); !cached {
			return m.startMarkdownRender(m.now(), true)
		}
	}
	return nil
}
