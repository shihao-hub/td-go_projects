package updater

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CheckResult 检查更新的结果
type CheckResult struct {
	CurrentVersion   string
	LatestVersion    string
	HasUpdate        bool
	TargetDir        string
	AlreadyInstalled bool
	ArchiveURL       string
	Sha256           string
	Cmd              string
}

// UpdateResult 执行更新的结果
type UpdateResult struct {
	CheckResult
	Updated bool
	Pruned  []string
}

// UpdateOptions 更新执行选项
type UpdateOptions struct {
	PreferLocal bool
	Force       bool
	KeepCount   int
	Progress    ProgressFunc
	Logger      *log.Logger
}

// Check 检查是否有 AGY 新版本
func Check(preferLocal bool) (*CheckResult, error) {
	entry, target, err := LoadRegistry(preferLocal)
	if err != nil {
		return nil, err
	}

	baseDir := DefaultAGYBaseDir()
	if baseDir == "" {
		return nil, fmt.Errorf("could not determine AGY base dir (LOCALAPPDATA empty)")
	}

	targetDir := VersionedArchiveCacheDir(baseDir, entry.Version, target.Archive, target.Sha256)

	// 获取本地已安装的最新版本
	currVer, _ := getCurrentInstalledVersion(baseDir)

	cmdName := target.Cmd
	if cmdName == "" {
		cmdName = "agy_acp_server.exe"
	}
	cmdName = strings.TrimPrefix(cmdName, "./")

	// 检查该目标目录是否已经完整存在
	installed := false
	if fi, err := os.Stat(filepath.Join(targetDir, cmdName)); err == nil && !fi.IsDir() {
		installed = true
	}

	hasUpdate := false
	if !installed {
		hasUpdate = true
	} else if currVer != "" && entry.Version != currVer {
		hasUpdate = true
	}

	return &CheckResult{
		CurrentVersion:   currVer,
		LatestVersion:    entry.Version,
		HasUpdate:        hasUpdate,
		TargetDir:        targetDir,
		AlreadyInstalled: installed,
		ArchiveURL:       target.Archive,
		Sha256:           target.Sha256,
		Cmd:              target.Cmd,
	}, nil
}

// Update 执行检查与更新
func Update(ctx context.Context, opts UpdateOptions) (*UpdateResult, error) {
	check, err := Check(opts.PreferLocal)
	if err != nil {
		return nil, fmt.Errorf("check update: %w", err)
	}

	res := &UpdateResult{
		CheckResult: *check,
		Updated:     false,
	}

	if check.AlreadyInstalled && !opts.Force {
		return res, nil
	}

	if !check.HasUpdate && !opts.Force {
		return res, nil
	}

	if opts.Logger != nil {
		opts.Logger.Printf("[updater] Downloading AGY %s from %s ...", check.LatestVersion, check.ArchiveURL)
	}

	dlOpts := DownloadOptions{
		ArchiveURL: check.ArchiveURL,
		Sha256:     check.Sha256,
		TargetDir:  check.TargetDir,
		Cmd:        check.Cmd,
		Timeout:    15 * time.Minute,
		Progress:   opts.Progress,
	}

	if err := DownloadAndExtract(ctx, dlOpts); err != nil {
		return nil, fmt.Errorf("download and extract: %w", err)
	}

	res.Updated = true
	res.AlreadyInstalled = true

	// 清理多余的历史旧版
	baseDir := DefaultAGYBaseDir()
	keep := opts.KeepCount
	if keep <= 0 {
		keep = 2
	}
	if pruned, pruneErr := PruneOldVersions(baseDir, keep); pruneErr == nil && len(pruned) > 0 {
		res.Pruned = pruned
		if opts.Logger != nil {
			opts.Logger.Printf("[updater] Pruned %d old versions: %v", len(pruned), pruned)
		}
	}

	return res, nil
}

// StartBackgroundLoop 后台静默循环检查更新（非阻塞）
func StartBackgroundLoop(logger *log.Logger, interval time.Duration, stopCh <-chan struct{}) {
	if interval <= 0 {
		interval = 4 * time.Hour
	}

	go func() {
		// 初始启动延时 15 秒，绝不抢占 Zed 启动与 panel 初始化资源
		select {
		case <-time.After(15 * time.Second):
		case <-stopCh:
			return
		}

		runOnce := func() {
			defer func() {
				if r := recover(); r != nil {
					if logger != nil {
						logger.Printf("[updater] Panic in background update: %v", r)
					}
				}
			}()

			check, err := Check(true)
			if err != nil {
				if logger != nil {
					logger.Printf("[updater] Background check skipped: %v", err)
				}
				return
			}

			if check.HasUpdate && !check.AlreadyInstalled {
				if logger != nil {
					logger.Printf("[updater] Found newer AGY version %s (current: %s). Starting background download...", check.LatestVersion, check.CurrentVersion)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
				defer cancel()

				opts := UpdateOptions{
					PreferLocal: true,
					KeepCount:   2,
					Logger:      logger,
				}
				res, upErr := Update(ctx, opts)
				if upErr != nil {
					if logger != nil {
						logger.Printf("[updater] Background update failed: %v", upErr)
					}
				} else if res.Updated {
					if logger != nil {
						logger.Printf("[updater] AGY successfully updated to %s at %s. Will be used on next worker cycle.", res.LatestVersion, res.TargetDir)
					}
				}
			}
		}

		// 运行首次静默检查
		runOnce()

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				runOnce()
			case <-stopCh:
				return
			}
		}
	}()
}

func getCurrentInstalledVersion(baseDir string) (string, bool) {
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return "", false
	}

	var latestName string
	var latestMod time.Time
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "v_") {
			fi, err := entry.Info()
			if err == nil {
				if latestName == "" || fi.ModTime().After(latestMod) {
					latestName = entry.Name()
					latestMod = fi.ModTime()
				}
			}
		}
	}

	if latestName == "" {
		return "", false
	}

	// 尝试从 v_{version}_{vh}_{ah} 中提取版本号
	parts := strings.Split(latestName, "_")
	if len(parts) >= 2 {
		return parts[1], true
	}
	return latestName, true
}
