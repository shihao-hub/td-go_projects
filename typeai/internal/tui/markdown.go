package tui

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"sync"

	"github.com/charmbracelet/glamour"
)

const (
	minRenderWidth = 20
	maxRenderWidth = 120
)

type markdownRenderer struct {
	mu       sync.Mutex
	width    int
	style    string
	renderer *glamour.TermRenderer
}

func newMarkdownRenderer(style string) *markdownRenderer {
	return &markdownRenderer{style: style}
}

func (r *markdownRenderer) Render(source string, width int, style string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	width = clampRenderWidth(width)
	if r.renderer == nil || r.width != width || r.style != style {
		options := []glamour.TermRendererOption{
			glamour.WithWordWrap(width),
		}
		if style == "notty" {
			options = append(options, glamour.WithStandardStyle("notty"))
		} else {
			options = append(options, glamour.WithAutoStyle())
		}
		renderer, err := glamour.NewTermRenderer(options...)
		if err != nil {
			return source, err
		}
		r.renderer = renderer
		r.width = width
		r.style = style
	}
	return r.renderer.Render(source)
}

type cacheKey struct {
	ID    string
	Hash  string
	Width int
	Style string
}

type markdownCache struct {
	mu    sync.Mutex
	items map[cacheKey]string
	order []cacheKey
	limit int
}

func newMarkdownCache() *markdownCache {
	return &markdownCache{
		items: make(map[cacheKey]string),
		limit: 256,
	}
}

func (c *markdownCache) get(key cacheKey) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	value, ok := c.items[key]
	return value, ok
}

func (c *markdownCache) put(key cacheKey, value string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.items[key]; !exists {
		c.order = append(c.order, key)
	}
	c.items[key] = value
	for len(c.order) > c.limit {
		oldest := c.order[0]
		c.order = c.order[1:]
		delete(c.items, oldest)
	}
}

func cacheKeyFor(id, source string, width int, style string) cacheKey {
	sum := sha256.Sum256([]byte(source))
	return cacheKey{
		ID:    id,
		Hash:  hex.EncodeToString(sum[:]),
		Width: clampRenderWidth(width),
		Style: style,
	}
}

func styleName(color bool) string {
	if os.Getenv("NO_COLOR") != "" || !color {
		return "notty"
	}
	return "auto"
}

func clampRenderWidth(width int) int {
	return min(max(width, minRenderWidth), maxRenderWidth)
}
