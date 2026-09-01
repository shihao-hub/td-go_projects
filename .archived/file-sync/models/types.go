package models

import (
	"time"

	"github.com/google/uuid"
)

type SyncTask struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	SourcePath  string    `json:"source_path"`
	TargetPath  string    `json:"target_path"`
	IgnoreRules []string  `json:"ignore_rules"`
	LastSync    time.Time `json:"last_sync"`
	Enabled     bool      `json:"enabled"`
}

func NewSyncTask(name, source, target string, rules []string) *SyncTask {
	if rules == nil {
		rules = []string{}
	}
	return &SyncTask{
		ID:          uuid.NewString(),
		Name:        name,
		SourcePath:  source,
		TargetPath:  target,
		IgnoreRules: rules,
		Enabled:     true,
	}
}

type FileDiff struct {
	Added    []string `json:"added"`
	Modified []string `json:"modified"`
	Deleted  []string `json:"deleted"`
}

func (d *FileDiff) Total() int {
	return len(d.Added) + len(d.Modified) + len(d.Deleted)
}

type SyncProgress struct {
	TaskID       string  `json:"task_id"`
	TotalFiles   int     `json:"total_files"`
	CurrentFile  int     `json:"current_file"`
	CurrentPath  string  `json:"current_path"`
	Status       string  `json:"status"`
	Percentage   float64 `json:"percentage"`
	ErrorMessage string  `json:"error_message,omitempty"`
}

type FileInfo struct {
	Path    string    `json:"path"`
	Hash    string    `json:"hash,omitempty"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mod_time"`
	IsDir   bool      `json:"is_dir"`
}

type FileTree struct {
	Files  map[string]*FileInfo `json:"files"`
	Errors []string             `json:"errors,omitempty"`
}
