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

// 同步各阶段状态。
const (
	StatusPending           = "pending"            // 已受理，未开始
	StatusScanning          = "scanning"           // 扫描/对比中
	StatusCopying           = "copying"            // 复制中
	StatusAwaitingDelete    = "awaiting_delete"    // 复制完成，等待用户确认删除
	StatusDeleting          = "deleting"           // 删除确认后执行中
	StatusCompleted         = "completed"          // 全部完成
	StatusError             = "error"              // 出错终止
	StatusCancelled         = "cancelled"          // 用户取消
)

// SyncProgress 是推送给前端的全量进度快照。
type SyncProgress struct {
	TaskID string `json:"task_id"`
	Status string `json:"status"`

	// 扫描阶段：总数未知，只有已发现计数。
	ScannedFiles int    `json:"scanned_files"`
	CurrentPath  string `json:"current_path"`

	// 复制/删除阶段：字节维度进度（复制）与条目维度进度（删除）。
	TotalFiles int    `json:"total_files"`
	DoneFiles  int    `json:"done_files"`
	TotalBytes int64  `json:"total_bytes"`
	DoneBytes  int64  `json:"done_bytes"`
	SpeedBPS   int64  `json:"speed_bps"`   // 滑动窗口平均速率，字节/秒
	ETASeconds int    `json:"eta_seconds"` // 预计剩余秒数，0 表示样本不足

	Percentage float64 `json:"percentage"`

	// 待删除文件清单，仅在 awaiting_delete 状态填充。
	PendingDeletes []string `json:"pending_deletes,omitempty"`

	ErrorMessage string `json:"error_message,omitempty"`
}

// Active 报告该状态是否属于"进行中"（前端据此禁用操作按钮）。
func (p SyncProgress) Active() bool {
	switch p.Status {
	case StatusPending, StatusScanning, StatusCopying, StatusAwaitingDelete, StatusDeleting:
		return true
	}
	return false
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
