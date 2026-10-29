package core

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"nutvault/internal/storage"
)

var (
	ErrFileNotFound = errors.New("源文件不存在")
	ErrFileTooLarge = errors.New("文件体积超出大小限制")
	ErrUnchanged    = errors.New("文件内容未变动")
)

const (
	DefaultNutstoreRoot     = `C:\Users\29580\Nutstore\1\我的坚果云`
	DefaultMaxFileSizeBytes = 20 * 1024 * 1024 // 默认 20MB
	DefaultScheduleInterval = 60              // 默认 60 分钟
)

type BackupResult struct {
	TargetID         int64
	SnapshotID       int64
	SourcePath       string
	SnapshotFilename string
	FullNutstorePath string
	FileSizeBytes    int64
	SHA256           string
	Status           string // success, skipped_unchanged, skipped_oversize, failed
	Message          string
}

type Engine struct {
	store *storage.Store
}

func NewEngine(store *storage.Store) *Engine {
	return &Engine{store: store}
}

// GetNutstoreRoot 获取当前配置的坚果云根目录
func (e *Engine) GetNutstoreRoot() string {
	root, err := e.store.GetConfig("nutstore_root", DefaultNutstoreRoot)
	if err != nil || root == "" {
		return DefaultNutstoreRoot
	}
	return root
}

// GetDefaultMaxSizeBytes 获取全局默认文件大小限制
func (e *Engine) GetDefaultMaxSizeBytes() int64 {
	valStr, err := e.store.GetConfig("max_file_size_bytes", strconv.FormatInt(DefaultMaxFileSizeBytes, 10))
	if err != nil {
		return DefaultMaxFileSizeBytes
	}
	val, err := strconv.ParseInt(valStr, 10, 64)
	if err != nil || val <= 0 {
		return DefaultMaxFileSizeBytes
	}
	return val
}

// CalculateFileHash 计算指定文件的 SHA256 与大小
func CalculateFileHash(filePath string) (string, int64, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return "", 0, err
	}
	if fi.IsDir() {
		return "", 0, fmt.Errorf("路径为目录而非文件: %s", filePath)
	}

	h := sha256.New()
	size, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), size, nil
}

// BackupTarget 执行对指定目标的备份
func (e *Engine) BackupTarget(target *storage.Target, force bool) (*BackupResult, error) {
	sourcePath := CleanPath(target.SourcePath)
	fi, err := os.Stat(sourcePath)
	if err != nil {
		if os.IsNotExist(err) {
			_, _ = e.store.RecordSnapshot(&storage.Snapshot{
				TargetID:         target.ID,
				SourcePath:       sourcePath,
				SnapshotFilename: "",
				FullNutstorePath: "",
				FileSizeBytes:    0,
				SHA256:           "",
				Status:           "failed",
				ErrorMessage:     ErrFileNotFound.Error(),
			})
			return nil, ErrFileNotFound
		}
		return nil, fmt.Errorf("读取文件状态失败: %w", err)
	}

	// 1. 大小门禁检查
	maxLimit := target.MaxSizeLimit
	if maxLimit <= 0 {
		maxLimit = e.GetDefaultMaxSizeBytes()
	}
	if fi.Size() > maxLimit {
		errMsg := fmt.Sprintf("文件大小 %d 字节超出上限 %d 字节", fi.Size(), maxLimit)
		_, _ = e.store.RecordSnapshot(&storage.Snapshot{
			TargetID:         target.ID,
			SourcePath:       sourcePath,
			SnapshotFilename: "",
			FullNutstorePath: "",
			FileSizeBytes:    fi.Size(),
			SHA256:           "",
			Status:           "skipped_oversize",
			ErrorMessage:     errMsg,
		})
		return &BackupResult{
			TargetID:      target.ID,
			SourcePath:    sourcePath,
			FileSizeBytes: fi.Size(),
			Status:        "skipped_oversize",
			Message:       errMsg,
		}, ErrFileTooLarge
	}

	// 2. 计算 SHA256 哈希
	hash, size, err := CalculateFileHash(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("计算哈希失败: %w", err)
	}

	// 3. 内容去重校验
	if !force && target.LastHash != "" && target.LastHash == hash {
		return &BackupResult{
			TargetID:      target.ID,
			SourcePath:    sourcePath,
			FileSizeBytes: size,
			SHA256:        hash,
			Status:        "skipped_unchanged",
			Message:       "文件未修改，跳过备份以节省坚果云流量",
		}, nil
	}

	// 4. 执行文件复制到坚果云
	nutstoreRoot := e.GetNutstoreRoot()
	targetDir := filepath.Join(nutstoreRoot, target.MappedDir)
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建坚果云目录失败: %w", err)
	}

	now := time.Now()
	snapshotFilename := FormatSnapshotFilename(target.FileName, now)
	fullNutstorePath := filepath.Join(targetDir, snapshotFilename)

	// 若同一秒内发生高频备份，防止同名覆盖
	if _, err := os.Stat(fullNutstorePath); err == nil {
		ext := filepath.Ext(snapshotFilename)
		stem := strings.TrimSuffix(snapshotFilename, ext)
		counter := 1
		for {
			candFilename := fmt.Sprintf("%s_%d%s", stem, counter, ext)
			candPath := filepath.Join(targetDir, candFilename)
			if _, err := os.Stat(candPath); os.IsNotExist(err) {
				snapshotFilename = candFilename
				fullNutstorePath = candPath
				break
			}
			counter++
		}
	}

	if err := copyFile(sourcePath, fullNutstorePath); err != nil {
		_, _ = e.store.RecordSnapshot(&storage.Snapshot{
			TargetID:         target.ID,
			SourcePath:       sourcePath,
			SnapshotFilename: snapshotFilename,
			FullNutstorePath: fullNutstorePath,
			FileSizeBytes:    size,
			SHA256:           hash,
			Status:           "failed",
			ErrorMessage:     err.Error(),
		})
		return nil, fmt.Errorf("复制文件至坚果云失败: %w", err)
	}

	// 5. 记录快照数据
	snap, err := e.store.RecordSnapshot(&storage.Snapshot{
		TargetID:         target.ID,
		SourcePath:       sourcePath,
		SnapshotFilename: snapshotFilename,
		FullNutstorePath: fullNutstorePath,
		FileSizeBytes:    size,
		SHA256:           hash,
		Status:           "success",
	})
	if err != nil {
		return nil, fmt.Errorf("记录快照失败: %w", err)
	}

	// 6. 更新 target 状态
	_ = e.store.UpdateTargetBackupState(target.ID, now, hash)

	// 7. 记录上传流量
	_ = e.store.RecordTraffic("backup", size, sourcePath)

	// 8. 版本清理
	if target.KeepVersions > 0 {
		e.pruneOldVersions(target)
	}

	return &BackupResult{
		TargetID:         target.ID,
		SnapshotID:       snap.ID,
		SourcePath:       sourcePath,
		SnapshotFilename: snapshotFilename,
		FullNutstorePath: fullNutstorePath,
		FileSizeBytes:    size,
		SHA256:           hash,
		Status:           "success",
		Message:          "备份成功",
	}, nil
}

// pruneOldVersions 清理超过保留上限的历史版本文件
func (e *Engine) pruneOldVersions(target *storage.Target) {
	snaps, err := e.store.ListSnapshotsForTarget(target.ID, 500)
	if err != nil {
		return
	}

	var successfulSnaps []storage.Snapshot
	for _, s := range snaps {
		if s.Status == "success" {
			successfulSnaps = append(successfulSnaps, s)
		}
	}

	if len(successfulSnaps) > target.KeepVersions {
		excess := successfulSnaps[target.KeepVersions:]
		for _, old := range excess {
			if old.FullNutstorePath != "" {
				_ = os.Remove(old.FullNutstorePath)
			}
		}
	}
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}
