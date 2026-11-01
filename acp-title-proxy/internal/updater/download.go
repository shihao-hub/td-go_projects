package updater

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ProgressFunc 进度回调函数（下载字节数，总字节数）
type ProgressFunc func(downloaded, total int64)

// DownloadOptions 下载与解压选项
type DownloadOptions struct {
	ArchiveURL string
	Sha256     string
	TargetDir  string
	Cmd        string
	Timeout    time.Duration
	Progress   ProgressFunc
}

// DownloadAndExtract 下载 zip 压缩包，验证 SHA-256 并解压到 TargetDir。
// 采用临时目录解压 + 原子重命名策略，杜绝半成品残留。
func DownloadAndExtract(ctx context.Context, opts DownloadOptions) error {
	if opts.Timeout == 0 {
		opts.Timeout = 15 * time.Minute
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	baseDir := filepath.Dir(opts.TargetDir)
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return fmt.Errorf("mkdir base dir %s: %w", baseDir, err)
	}

	// 1. 创建临时下载与解压工作区
	tmpName := fmt.Sprintf(".tmp-agy-%d", time.Now().UnixNano())
	tmpExtractDir := filepath.Join(baseDir, tmpName)
	if err := os.MkdirAll(tmpExtractDir, 0755); err != nil {
		return fmt.Errorf("create temp extract dir: %w", err)
	}
	defer func() {
		// 如果未成功完成，清理临时目录
		_ = os.RemoveAll(tmpExtractDir)
	}()

	tmpZipFile := filepath.Join(baseDir, tmpName+".zip")
	defer func() {
		_ = os.Remove(tmpZipFile)
	}()

	// 2. 发起 HTTP 下载
	client := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
		},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, opts.ArchiveURL, nil)
	if err != nil {
		return fmt.Errorf("new download request: %w", err)
	}
	req.Header.Set("User-Agent", "acp-title-proxy-updater")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download request %s: %w", opts.ArchiveURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	outZip, err := os.Create(tmpZipFile)
	if err != nil {
		return fmt.Errorf("create temp zip file: %w", err)
	}

	hasher := sha256.New()
	writer := io.MultiWriter(outZip, hasher)

	totalBytes := resp.ContentLength
	var downloaded int64

	buf := make([]byte, 64*1024)
	for {
		select {
		case <-ctx.Done():
			outZip.Close()
			return ctx.Err()
		default:
		}

		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, wErr := writer.Write(buf[:n]); wErr != nil {
				outZip.Close()
				return fmt.Errorf("write temp zip: %w", wErr)
			}
			downloaded += int64(n)
			if opts.Progress != nil {
				opts.Progress(downloaded, totalBytes)
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			outZip.Close()
			return fmt.Errorf("read response body: %w", readErr)
		}
	}
	_ = outZip.Close()

	// 3. 校验 SHA-256
	if opts.Sha256 != "" {
		actualHash := hex.EncodeToString(hasher.Sum(nil))
		if !strings.EqualFold(actualHash, opts.Sha256) {
			return fmt.Errorf("sha256 mismatch: got %s, want %s", actualHash, opts.Sha256)
		}
	}

	// 4. 解压 zip 到临时目录
	if err := unzipFile(tmpZipFile, tmpExtractDir); err != nil {
		return fmt.Errorf("unzip archive: %w", err)
	}

	// 5. 校验关键可执行文件是否存在
	cmdName := opts.Cmd
	if cmdName == "" {
		cmdName = "agy_acp_server.exe"
	}
	cmdName = strings.TrimPrefix(cmdName, "./")
	exeCheck := filepath.Join(tmpExtractDir, cmdName)
	if _, err := os.Stat(exeCheck); err != nil {
		return fmt.Errorf("extracted contents missing binary %s: %w", cmdName, err)
	}

	// 6. 原子重命名到正式目标目录
	// 如果目标目录已在其他进程中生成，先测试是否存在
	if fi, err := os.Stat(opts.TargetDir); err == nil && fi.IsDir() {
		// 已经存在，说明其他进程或 Zed 刚刚已拉取成功
		return nil
	}

	if err := os.Rename(tmpExtractDir, opts.TargetDir); err != nil {
		return fmt.Errorf("rename to target dir %s: %w", opts.TargetDir, err)
	}

	return nil
}

// unzipFile 解压 zip 文件到指定目录（防 ZipSlip 漏洞）
func unzipFile(srcZip, destDir string) error {
	r, err := zip.OpenReader(srcZip)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		// 校验防路径穿越
		targetPath := filepath.Join(destDir, f.Name)
		if !strings.HasPrefix(filepath.Clean(targetPath), filepath.Clean(destDir)+string(os.PathSeparator)) && filepath.Clean(targetPath) != filepath.Clean(destDir) {
			return fmt.Errorf("illegal file path in zip: %s", f.Name)
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(targetPath, f.Mode()); err != nil {
				return err
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
			return err
		}

		outFile, err := os.OpenFile(targetPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			outFile.Close()
			return err
		}

		_, copyErr := io.Copy(outFile, rc)
		rc.Close()
		outFile.Close()

		if copyErr != nil {
			return copyErr
		}
	}

	return nil
}

// PruneOldVersions 保留 baseDir 下最新的 keepCount 个 v_* 目录，删除其余老旧版本。
func PruneOldVersions(baseDir string, keepCount int) ([]string, error) {
	if keepCount <= 0 {
		keepCount = 2
	}

	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return nil, err
	}

	type versionItem struct {
		name    string
		path    string
		modTime time.Time
	}

	var items []versionItem
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "v_") {
			fullPath := filepath.Join(baseDir, entry.Name())
			fi, err := entry.Info()
			if err == nil {
				items = append(items, versionItem{
					name:    entry.Name(),
					path:    fullPath,
					modTime: fi.ModTime(),
				})
			}
		}
	}

	if len(items) <= keepCount {
		return nil, nil
	}

	// 按修改时间从新到旧排序
	sort.Slice(items, func(i, j int) bool {
		return items[i].modTime.After(items[j].modTime)
	})

	var removed []string
	for i := keepCount; i < len(items); i++ {
		target := items[i].path
		if err := os.RemoveAll(target); err == nil {
			removed = append(removed, items[i].name)
		}
	}

	return removed, nil
}
