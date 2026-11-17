// Package audit 记录邮件处理审计事件（JSONL）。
//
// 只记录元信息：UID、发件人、主题、命中规则/丢弃原因、投递结果与耗时。
// 严禁写入邮件正文、授权码、Webhook 或签名密钥。
package audit

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	// FileName 是当前审计日志文件名；轮转文件追加 .1/.2/.3 后缀。
	FileName = "audit.jsonl"

	defaultMaxBytes = 10 << 20
	defaultBackups  = 3
)

// 事件类型。
const (
	TypeMail = "mail"
	TypePush = "push"
)

// 处理动作。
const (
	ActionWhitelisted = "whitelisted"
	ActionDropped     = "dropped"
	ActionParseFailed = "parse_failed"
	// ActionCooldownDropped 表示邮件命中白名单，但因同讨论串仍在冷却期内被有意丢弃。
	ActionCooldownDropped = "cooldown_dropped"
	ActionPushed          = "pushed"
	ActionPushFailed      = "push_failed"
)

// Event 是一条审计事件。Type=mail 时记录接收与过滤结果；
// Type=push 时记录一次防抖窗口结束后的投递结果。
type Event struct {
	Time       time.Time `json:"time"`
	Type       string    `json:"type"`
	UID        uint32    `json:"uid,omitempty"`
	From       string    `json:"from,omitempty"`
	Subject    string    `json:"subject,omitempty"`
	Action     string    `json:"action"`
	Rule       string    `json:"rule,omitempty"`
	Reason     string    `json:"reason,omitempty"`
	Thread     string    `json:"thread,omitempty"`
	OK         *bool     `json:"ok,omitempty"`
	DurationMS int64     `json:"duration_ms,omitempty"`
	Error      string    `json:"error,omitempty"`
}

// Logger 是线程安全的 JSONL 审计写入器，按大小轮转。
type Logger struct {
	path     string
	maxBytes int64
	backups  int

	mu   sync.Mutex
	file *os.File
	size int64
}

// Open 打开（或创建）目录下的 audit.jsonl。
// maxBytes <= 0 时使用默认 10 MiB；backups <= 0 时使用默认 3 份。
func Open(dir string, maxBytes int64, backups int) (*Logger, error) {
	if maxBytes <= 0 {
		maxBytes = defaultMaxBytes
	}
	if backups <= 0 {
		backups = defaultBackups
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("创建审计目录失败: %w", err)
	}
	path := filepath.Join(dir, FileName)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("打开审计日志失败: %w", err)
	}
	size := int64(0)
	if info, err := file.Stat(); err == nil {
		size = info.Size()
	}
	return &Logger{path: path, maxBytes: maxBytes, backups: backups, file: file, size: size}, nil
}

// OpenDefault 使用默认轮转参数打开审计日志。
func OpenDefault(dir string) (*Logger, error) {
	return Open(dir, defaultMaxBytes, defaultBackups)
}

// Append 追加一条事件。Time 为零值时自动填当前 UTC 时间。
func (l *Logger) Append(event Event) error {
	if event.Time.IsZero() {
		event.Time = time.Now().UTC()
	}
	line, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("序列化审计事件失败: %w", err)
	}
	line = append(line, '\n')

	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return errors.New("审计日志已关闭")
	}
	if l.maxBytes > 0 && l.size+int64(len(line)) > l.maxBytes {
		if err := l.rotateLocked(); err != nil {
			return err
		}
	}
	written, err := l.file.Write(line)
	l.size += int64(written)
	if err != nil {
		return fmt.Errorf("写入审计日志失败: %w", err)
	}
	return nil
}

func (l *Logger) rotateLocked() error {
	if err := l.file.Close(); err != nil {
		return fmt.Errorf("关闭审计日志失败: %w", err)
	}
	l.file = nil

	_ = os.Remove(fmt.Sprintf("%s.%d", l.path, l.backups))
	for i := l.backups - 1; i >= 1; i-- {
		_ = os.Rename(fmt.Sprintf("%s.%d", l.path, i), fmt.Sprintf("%s.%d", l.path, i+1))
	}
	_ = os.Rename(l.path, l.path+".1")

	file, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("轮转后重新打开审计日志失败: %w", err)
	}
	l.file = file
	l.size = 0
	return nil
}

// Close 关闭文件；重复调用安全。
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return nil
	}
	err := l.file.Close()
	l.file = nil
	return err
}

// ReadTail 读取当前审计文件最后 limit 条可解析事件（时间升序）。
// 轮转出的历史文件不参与读取，保留给人工排查。
func ReadTail(dir string, limit int) ([]Event, error) {
	if limit <= 0 {
		return nil, nil
	}
	data, err := os.ReadFile(filepath.Join(dir, FileName))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取审计日志失败: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	events := make([]Event, 0, limit)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var event Event
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue // 跳过断电或并发写入留下的坏行
		}
		events = append(events, event)
	}
	if len(events) > limit {
		events = events[len(events)-limit:]
	}
	return events, nil
}
