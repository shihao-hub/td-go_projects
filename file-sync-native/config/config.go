package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	stdsync "sync"

	"file-sync-native/models"
)

// Config 与旧版 file-sync 共享 ~/.file-sync/config.json，任务配置互通。
type Config struct {
	mu         stdsync.Mutex
	path       string
	BackupRoot string              `json:"backup_root,omitempty"`
	Tasks      []*models.SyncTask `json:"tasks"`
}

func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".file-sync", "config.json"), nil
}

func Load(path string) (*Config, error) {
	if path == "" {
		p, err := DefaultPath()
		if err != nil {
			return nil, err
		}
		path = p
	}
	c := &Config{path: path, Tasks: []*models.SyncTask{}}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return c, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, c); err != nil {
		return nil, err
	}
	if c.Tasks == nil {
		c.Tasks = []*models.SyncTask{}
	}
	return c, nil
}

func (c *Config) Path() string {
	return c.path
}

func (c *Config) Save() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.saveLocked()
}

func (c *Config) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, c.path)
}

var ErrTaskNotFound = errors.New("任务不存在")

func (c *Config) AddTask(task *models.SyncTask) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Tasks = append(c.Tasks, task)
	return c.saveLocked()
}

func (c *Config) UpdateTask(task *models.SyncTask) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, t := range c.Tasks {
		if t.ID == task.ID {
			c.Tasks[i] = task
			return c.saveLocked()
		}
	}
	return ErrTaskNotFound
}

func (c *Config) DeleteTask(id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, t := range c.Tasks {
		if t.ID == id {
			c.Tasks = append(c.Tasks[:i], c.Tasks[i+1:]...)
			return c.saveLocked()
		}
	}
	return ErrTaskNotFound
}

func (c *Config) GetTask(id string) (*models.SyncTask, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, t := range c.Tasks {
		if t.ID == id {
			return t, nil
		}
	}
	return nil, ErrTaskNotFound
}

func (c *Config) ListTasks() []*models.SyncTask {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]*models.SyncTask, len(c.Tasks))
	copy(out, c.Tasks)
	return out
}

func (c *Config) GetBackupRoot() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.BackupRoot
}

func (c *Config) SetBackupRoot(dir string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.BackupRoot = dir
	return c.saveLocked()
}
