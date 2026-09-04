package ui

import (
	"strings"
	"sync"
)

// LogBuffer 环形日志缓冲：接住 aiquickd 的 stderr，
// 供「日志」对话框查看。实现 io.Writer。
type LogBuffer struct {
	mu    sync.Mutex
	lines []string
	max   int
}

func NewLogBuffer(max int) *LogBuffer {
	return &LogBuffer{max: max}
}

func (b *LogBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := string(p)
	for _, line := range strings.Split(strings.TrimRight(s, "\r\n"), "\n") {
		if line == "" {
			continue
		}
		b.lines = append(b.lines, strings.TrimRight(line, "\r"))
	}
	if over := len(b.lines) - b.max; over > 0 {
		b.lines = b.lines[over:]
	}
	return len(p), nil
}

// AppendString 直接追加一行（非 io.Writer 来源）。
func (b *LogBuffer) AppendString(s string) {
	_, _ = b.Write([]byte(s + "\n"))
}

func (b *LogBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.Join(b.lines, "\n")
}
