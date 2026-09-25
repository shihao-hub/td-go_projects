// Package session 负责把一次进程内对话持久化为人类可读 JSON。
package session

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// SchemaVersion 是 session JSON 的当前结构版本。
const SchemaVersion = 1

// Message 是落盘的一条对话消息。
type Message struct {
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

// Session 是单个 typeai 进程对应的完整会话。
type Session struct {
	SchemaVersion int       `json:"schema_version"`
	ID            string    `json:"id"`
	Model         string    `json:"model"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	Messages      []Message `json:"messages"`
}

// Store 管理一个 session 文件；创建时不写盘，首轮成功回答后才落盘。
type Store struct {
	dir  string
	path string
	id   string
	mu   sync.Mutex
}

// New 创建 session 存储并预生成稳定文件名。
func New(dir string, now time.Time) (*Store, error) {
	id, err := randomID()
	if err != nil {
		return nil, err
	}
	return &Store{
		dir:  filepath.Join(dir, "sessions"),
		path: filepath.Join(dir, "sessions", now.Format("20060102-150405")+"-"+id+".json"),
		id:   id,
	}, nil
}

// Path 返回 session JSON 的完整路径。
func (s *Store) Path() string { return s.path }

// ID 返回本次进程内会话的短随机 ID。
func (s *Store) ID() string { return s.id }

// Save 把完整会话序列化后原子替换目标文件。调用方负责在成功后更新内存历史。
func (s *Store) Save(session Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	raw, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化会话失败: %w", err)
	}
	raw = append(raw, '\n')
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return fmt.Errorf("创建会话目录失败: %w", err)
	}
	return writeFileAtomic(s.path, raw, 0o600)
}

func randomID() (string, error) {
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("生成会话 ID 失败: %w", err)
	}
	return hex.EncodeToString(buf[:]), nil
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-")
	if err != nil {
		return fmt.Errorf("创建会话临时文件失败: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("写入会话临时文件失败: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("同步会话临时文件失败: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("关闭会话临时文件失败: %w", err)
	}
	if err := os.Chmod(tmpName, mode); err != nil {
		return fmt.Errorf("设置会话文件权限失败: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("替换会话文件失败: %w", err)
	}
	return nil
}
