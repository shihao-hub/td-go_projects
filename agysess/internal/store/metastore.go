package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// SessionMeta 自有元数据结构
type SessionMeta struct {
	IsArchived  bool       `json:"is_archived"`
	ArchivedAt  *time.Time `json:"archived_at,omitempty"`
	CustomNotes string     `json:"custom_notes,omitempty"`
	CustomTitle string     `json:"custom_title,omitempty"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// MetaDataMap 内存索引映射：SessionID -> SessionMeta
type MetaDataMap struct {
	Sessions map[string]SessionMeta `json:"sessions"`
}

// MetaStore 管理自有会话元数据存储，严格落盘在 %APPDATA%/language_projects/agysess/
type MetaStore struct {
	mu       sync.RWMutex
	filePath string
	data     MetaDataMap
}

// NewMetaStore 初始化 MetaStore 并自动创建完整目录链
func NewMetaStore(customPath string) (*MetaStore, error) {
	filePath := customPath
	if filePath == "" {
		appData := os.Getenv("APPDATA")
		if appData == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return nil, fmt.Errorf("无法获取家目录以初始化数据路径: %w", err)
			}
			filePath = filepath.Join(home, ".language_projects", "agysess", "sessions_meta.json")
		} else {
			filePath = filepath.Join(appData, "language_projects", "agysess", "sessions_meta.json")
		}
	}

	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("创建自有数据目录链失败 [%s]: %w", dir, err)
	}

	store := &MetaStore{
		filePath: filePath,
		data: MetaDataMap{
			Sessions: make(map[string]SessionMeta),
		},
	}

	if err := store.load(); err != nil {
		// 文件不存在时为初次启动，不视为致命错误
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("加载自有元数据文件失败 [%s]: %w", filePath, err)
		}
	}

	return store, nil
}

// FilePath 获取持久化文件路径
func (m *MetaStore) FilePath() string {
	return m.filePath
}

func (m *MetaStore) load() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	bytes, err := os.ReadFile(m.filePath)
	if err != nil {
		return err
	}

	var data MetaDataMap
	if err := json.Unmarshal(bytes, &data); err != nil {
		return fmt.Errorf("反序列化元数据 JSON 失败: %w", err)
	}

	if data.Sessions == nil {
		data.Sessions = make(map[string]SessionMeta)
	}
	m.data = data
	return nil
}

func (m *MetaStore) save() error {
	bytes, err := json.MarshalIndent(m.data, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化元数据失败: %w", err)
	}

	// 写入临时文件再原子重命名，防止写入中断导致损坏
	tmpPath := m.filePath + ".tmp"
	if err := os.WriteFile(tmpPath, bytes, 0644); err != nil {
		return fmt.Errorf("写入临时元数据文件失败: %w", err)
	}

	if err := os.Rename(tmpPath, m.filePath); err != nil {
		// Windows 平台 rename 若目标已存在可能会失败，尝试先删除
		_ = os.Remove(m.filePath)
		if err := os.Rename(tmpPath, m.filePath); err != nil {
			return fmt.Errorf("原子替换元数据文件失败: %w", err)
		}
	}

	return nil
}

// GetMeta 获取指定会话的元数据
func (m *MetaStore) GetMeta(sessionID string) (SessionMeta, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	meta, ok := m.data.Sessions[sessionID]
	return meta, ok
}

// SetArchived 标记会话归档或取消归档
func (m *MetaStore) SetArchived(sessionID string, archived bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	meta := m.data.Sessions[sessionID]
	meta.IsArchived = archived
	meta.UpdatedAt = time.Now()
	if archived {
		now := time.Now()
		meta.ArchivedAt = &now
	} else {
		meta.ArchivedAt = nil
	}

	m.data.Sessions[sessionID] = meta
	return m.save()
}

// SetNotes 设置自定义备注
func (m *MetaStore) SetNotes(sessionID string, notes string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	meta := m.data.Sessions[sessionID]
	meta.CustomNotes = notes
	meta.UpdatedAt = time.Now()

	m.data.Sessions[sessionID] = meta
	return m.save()
}

// SetCustomTitle 设置自定义标题
func (m *MetaStore) SetCustomTitle(sessionID string, title string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	meta := m.data.Sessions[sessionID]
	meta.CustomTitle = title
	meta.UpdatedAt = time.Now()

	m.data.Sessions[sessionID] = meta
	return m.save()
}

// GetAllMeta 返回全部已存元数据的副本
func (m *MetaStore) GetAllMeta() map[string]SessionMeta {
	m.mu.RLock()
	defer m.mu.RUnlock()

	res := make(map[string]SessionMeta, len(m.data.Sessions))
	for k, v := range m.data.Sessions {
		res[k] = v
	}
	return res
}
