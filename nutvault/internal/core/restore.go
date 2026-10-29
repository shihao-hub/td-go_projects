package core

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type RestoreResult struct {
	SnapshotID     int64  `json:"snapshot_id"`
	SourcePath     string `json:"source_path"`
	RestoredFrom   string `json:"restored_from"`
	PreRestorePath string `json:"pre_restore_path,omitempty"`
	FileSizeBytes  int64  `json:"file_size_bytes"`
	SHA256         string `json:"sha256"`
	Message        string `json:"message"`
}

// RestoreSnapshot 将指定快照恢复到其原始源文件路径或指定目标路径
func (e *Engine) RestoreSnapshot(snapshotID int64, customTargetPath string, force bool) (*RestoreResult, error) {
	snap, err := e.store.GetSnapshotByID(snapshotID)
	if err != nil {
		return nil, fmt.Errorf("查找快照记录失败: %w", err)
	}

	// 1. 验证坚果云快照文件是否存在
	if _, err := os.Stat(snap.FullNutstorePath); err != nil {
		return nil, fmt.Errorf("坚果云快照文件不存在或已被移动: %s", snap.FullNutstorePath)
	}

	targetPath := snap.SourcePath
	if customTargetPath != "" {
		targetPath = CleanPath(customTargetPath)
	}

	// 2. 检查源文件，若存在则创建安全备份副本
	var preRestorePath string
	if _, err := os.Stat(targetPath); err == nil {
		timestamp := time.Now().Format("20060102150405")
		preRestorePath = fmt.Sprintf("%s.%s_pre_restore.bak", targetPath, timestamp)
		if err := copyFile(targetPath, preRestorePath); err != nil {
			return nil, fmt.Errorf("创建源文件恢复前安全副本失败: %w", err)
		}
	}

	// 3. 确保目标目录存在
	targetDir := filepath.Dir(targetPath)
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建目标目录失败: %w", err)
	}

	// 4. 将坚果云快照覆盖复制到目标路径
	if err := copyFile(snap.FullNutstorePath, targetPath); err != nil {
		return nil, fmt.Errorf("恢复写入文件失败: %w", err)
	}

	// 5. 计算当前恢复后文件的 SHA256 与大小
	hash, size, _ := CalculateFileHash(targetPath)

	// 6. 记录下载流量
	_ = e.store.RecordTraffic("restore", size, targetPath)

	// 7. 更新 target 的最新状态
	if snap.TargetID > 0 {
		_ = e.store.UpdateTargetBackupState(snap.TargetID, time.Now(), hash)
	}

	return &RestoreResult{
		SnapshotID:     snap.ID,
		SourcePath:     targetPath,
		RestoredFrom:   snap.FullNutstorePath,
		PreRestorePath: preRestorePath,
		FileSizeBytes:  size,
		SHA256:         hash,
		Message:        "恢复成功",
	}, nil
}
