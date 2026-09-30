// Package session 负责把一次进程内对话持久化为人类可读 JSON。
package session

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// SchemaVersion 是 session JSON 的当前结构版本。
const SchemaVersion = 3

// Message 是落盘的一条对话消息。
type Message struct {
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	Images    []Image   `json:"images,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// Image 保存发送给模型的本地图元数据；base64 不落盘。
type Image struct {
	Path      string `json:"path"`
	FileName  string `json:"file_name"`
	MediaType string `json:"media_type"`
	Size      int64  `json:"size"`
}

// Session 是单个 typeai 进程对应的完整会话。
type Session struct {
	SchemaVersion  int       `json:"schema_version"`
	ID             string    `json:"id"`
	Model          string    `json:"model"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	RootBranchID   string    `json:"root_branch_id,omitempty"`
	ActiveBranchID string    `json:"active_branch_id,omitempty"`
	Branches       []Branch  `json:"branches,omitempty"`
	Messages       []Message `json:"messages,omitempty"`
}

// SessionSummary 是会话选择列表使用的只读快照。
type SessionSummary struct {
	ID            string
	Path          string
	Model         string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	FirstUser     string
	LastAssistant string
	MessageCount  int
	Branches      []string
}

// Normalize 规范化 Session 结构，将 schema v2 自动迁移到 v3，并验证分支树结构。
func (s *Session) Normalize() error {
	if s.SchemaVersion < 3 || len(s.Branches) == 0 {
		if s.RootBranchID == "" {
			s.RootBranchID = DefaultRootBranchID
		}
		if s.ActiveBranchID == "" {
			s.ActiveBranchID = s.RootBranchID
		}
		if len(s.Branches) == 0 {
			s.Branches = []Branch{
				{
					ID:               s.RootBranchID,
					ParentID:         "",
					ForkMessageIndex: 0,
					Messages:         s.Messages,
					CreatedAt:        s.CreatedAt,
				},
			}
		}
		s.SchemaVersion = SchemaVersion
	}
	if s.RootBranchID == "" {
		s.RootBranchID = DefaultRootBranchID
	}
	if s.ActiveBranchID == "" {
		s.ActiveBranchID = s.RootBranchID
	}

	if err := ValidateBranches(s.RootBranchID, s.Branches); err != nil {
		return err
	}

	// 保持内存中 Messages 与激活分支解析后的历史一致，兼容旧调用
	if resolved, err := ResolveMessages(s.ActiveBranchID, s.Branches); err == nil {
		s.Messages = resolved
	}
	return nil
}

// UnmarshalJSON 自定义反序列化，自动将旧版本会话迁移为 v3 分支树结构。
func (s *Session) UnmarshalJSON(data []byte) error {
	type alias Session
	var aux alias
	if err := json.Unmarshal(data, &aux); err != nil {
		return fmt.Errorf("解析会话 JSON 失败: %w", err)
	}
	*s = Session(aux)
	return s.Normalize()
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

	if err := session.Normalize(); err != nil {
		return fmt.Errorf("会话结构非法: %w", err)
	}

	// 落盘时剥离顶层冗余的 messages，保持 schema v3 干净分支树
	toSave := session
	toSave.Messages = nil
	toSave.SchemaVersion = SchemaVersion

	raw, err := json.MarshalIndent(toSave, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化会话失败: %w", err)
	}
	raw = append(raw, '\n')
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return fmt.Errorf("创建会话目录失败: %w", err)
	}
	return writeFileAtomic(s.path, raw, 0o600)
}

// IsValidSessionID 校验是否为合法的 8 位小写十六进制 session ID。
func IsValidSessionID(id string) bool {
	if len(id) != 8 {
		return false
	}
	for i := 0; i < len(id); i++ {
		b := id[i]
		if !((b >= '0' && b <= '9') || (b >= 'a' && b <= 'f')) {
			return false
		}
	}
	return true
}

// LoadByID 从 sessions 目录加载指定 session ID 的持久化会话。
func LoadByID(dataDir string, id string) (*Store, *Session, error) {
	if !IsValidSessionID(id) {
		return nil, nil, fmt.Errorf("非法 session ID: 必须为 8 位小写十六进制字符")
	}

	sessionsDir := filepath.Join(dataDir, "sessions")
	pattern := filepath.Join(sessionsDir, "*-"+id+".json")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, nil, fmt.Errorf("查找会话文件失败: %w", err)
	}
	if len(matches) == 0 {
		return nil, nil, fmt.Errorf("未找到 session ID 为 %s 的会话", id)
	}
	if len(matches) > 1 {
		return nil, nil, fmt.Errorf("存在多个匹配 session ID 为 %s 的会话", id)
	}

	targetPath := matches[0]
	raw, err := os.ReadFile(targetPath)
	if err != nil {
		return nil, nil, fmt.Errorf("读取会话文件失败: %w", err)
	}

	var sess Session
	if err := json.Unmarshal(raw, &sess); err != nil {
		return nil, nil, fmt.Errorf("解析会话失败: %w", err)
	}

	store := &Store{
		dir:  sessionsDir,
		path: targetPath,
		id:   sess.ID,
	}
	return store, &sess, nil
}

// ListSessions 枚举 sessions 目录中的完整会话，并按最近更新时间降序返回摘要。
func ListSessions(dataDir string) ([]SessionSummary, error) {
	sessionsDir := filepath.Join(dataDir, "sessions")
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []SessionSummary{}, nil
		}
		return nil, fmt.Errorf("读取会话目录失败: %w", err)
	}

	summaries := make([]SessionSummary, 0)
	seen := make(map[string]bool)
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(sessionsDir, entry.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		var sess Session
		if err := json.Unmarshal(raw, &sess); err != nil {
			continue
		}
		if !IsValidSessionID(sess.ID) || seen[sess.ID] {
			continue
		}
		history, err := ResolveMessages(sess.ActiveBranchID, sess.Branches)
		if err != nil {
			continue
		}
		seen[sess.ID] = true
		summaries = append(summaries, SessionSummary{
			ID:            sess.ID,
			Path:          path,
			Model:         sess.Model,
			CreatedAt:     sess.CreatedAt,
			UpdatedAt:     sess.UpdatedAt,
			FirstUser:     firstMessageText(history, "user"),
			LastAssistant: lastMessageText(history, "assistant"),
			MessageCount:  len(history),
			Branches:      branchIDs(sess.Branches),
		})
	}

	sort.SliceStable(summaries, func(i, j int) bool {
		if !summaries[i].UpdatedAt.Equal(summaries[j].UpdatedAt) {
			return summaries[i].UpdatedAt.After(summaries[j].UpdatedAt)
		}
		return summaries[i].Path < summaries[j].Path
	})
	return summaries, nil
}

func firstMessageText(messages []Message, role string) string {
	for _, message := range messages {
		if message.Role == role {
			return summarizeMessage(message)
		}
	}
	return ""
}

func lastMessageText(messages []Message, role string) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == role {
			return summarizeMessage(messages[i])
		}
	}
	return ""
}

func summarizeMessage(message Message) string {
	text := strings.Join(strings.Fields(message.Content), " ")
	runes := []rune(text)
	const limit = 120
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "..."
}

func branchIDs(branches []Branch) []string {
	ids := make([]string, 0, len(branches))
	for _, branch := range branches {
		ids = append(ids, branch.ID)
	}
	return ids
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
