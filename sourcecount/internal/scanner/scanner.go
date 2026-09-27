// Package scanner 负责目录遍历、过滤、文件分类和单文件指标采集。
package scanner

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"sync"

	"sourcecount/internal/config"
	"sourcecount/internal/contract"
)

// Options 是扫描器的已解析配置。
type Options struct {
	Roots            []string
	Matcher          config.Matcher
	TextExtensions   map[string]struct{}
	BinaryExtensions map[string]struct{}
	Workers          int
}

// Result 是一个文件的统计或错误。
type Result struct {
	Path      string
	Extension string
	Kind      Kind
	Text      contract.TextStats
	Binary    contract.BinaryStats
	Error     *contract.ScanError
}

type fileJob struct {
	path      string
	extension string
	size      int64
	preError  *contract.ScanError
}

// Scan 扫描所有根路径。单文件错误放入 Result.Error；根路径遍历错误作为
// 返回错误，避免把无法确认的整棵目录树伪装成空结果。
func Scan(ctx context.Context, opts Options) ([]Result, error) {
	workers := opts.Workers
	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0)
		if workers > 8 {
			workers = 8
		}
		if workers < 1 {
			workers = 1
		}
	}

	jobs := make(chan fileJob)
	results := make(chan Result)
	var workersWG sync.WaitGroup
	for i := 0; i < workers; i++ {
		workersWG.Add(1)
		go func() {
			defer workersWG.Done()
			for job := range jobs {
				result := process(job, opts)
				select {
				case results <- result:
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	var collected []Result
	collectDone := make(chan struct{})
	go func() {
		defer close(collectDone)
		for result := range results {
			collected = append(collected, result)
		}
	}()

	seenFiles := make(map[string]struct{})
	var walkErr error
	for _, root := range opts.Roots {
		if err := walkRoot(ctx, root, opts, jobs, seenFiles); err != nil {
			walkErr = err
			break
		}
	}
	close(jobs)
	workersWG.Wait()
	close(results)
	<-collectDone
	if walkErr != nil {
		return nil, walkErr
	}
	sort.Slice(collected, func(i, j int) bool { return pathKey(collected[i].Path) < pathKey(collected[j].Path) })
	return collected, nil
}

func walkRoot(ctx context.Context, root string, opts Options, jobs chan<- fileJob, seenFiles map[string]struct{}) error {
	info, err := os.Lstat(root)
	if err != nil {
		return err
	}
	if isLink(info) {
		return nil
	}
	if !info.IsDir() {
		if !info.Mode().IsRegular() {
			return nil
		}
		rel := filepath.ToSlash(filepath.Base(root))
		if opts.Matcher.ExcludeFile(rel) || !opts.Matcher.IncludeFile(rel) || !markSeen(seenFiles, root) {
			return nil
		}
		return sendJob(ctx, jobs, fileJob{path: root, extension: extensionFor(root), size: info.Size()})
	}

	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			if path == root {
				return walkErr
			}
			return sendResultError(ctx, jobs, path, "walk_failed", walkErr)
		}
		if path == root {
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return sendResultError(ctx, jobs, path, "stat_failed", infoErr)
		}
		if isLink(info) {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return sendResultError(ctx, jobs, path, "path_failed", relErr)
		}
		rel = filepath.ToSlash(rel)
		if entry.IsDir() {
			if opts.Matcher.ExcludeDir(rel) || !opts.Matcher.IncludeDir(rel) {
				return fs.SkipDir
			}
			return nil
		}
		if !info.Mode().IsRegular() || opts.Matcher.ExcludeFile(rel) || !opts.Matcher.IncludeFile(rel) || !markSeen(seenFiles, path) {
			return nil
		}
		return sendJob(ctx, jobs, fileJob{path: path, extension: extensionFor(path), size: info.Size()})
	})
}

func process(job fileJob, opts Options) Result {
	result := Result{Path: job.path, Extension: job.extension, Error: job.preError}
	if job.preError != nil {
		return result
	}
	kind, err := Classify(job.path, job.extension, opts.TextExtensions, opts.BinaryExtensions)
	if err != nil {
		result.Error = fileError(job.path, "classify_failed", err)
		return result
	}
	result.Kind = kind
	if kind == KindBinary {
		result.Binary = contract.BinaryStats{Files: 1, Bytes: job.size}
		return result
	}
	stats, err := CountTextFile(job.path)
	if err != nil {
		result.Error = fileError(job.path, "read_failed", err)
		return result
	}
	stats.Files = 1
	result.Text = stats
	return result
}

func sendJob(ctx context.Context, jobs chan<- fileJob, job fileJob) error {
	select {
	case jobs <- job:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func sendResultError(ctx context.Context, jobs chan<- fileJob, path, code string, err error) error {
	return sendJob(ctx, jobs, fileJob{
		path:     path,
		preError: fileError(path, code, err),
	})
}

func fileError(path, code string, err error) *contract.ScanError {
	return &contract.ScanError{Path: path, Code: code, Message: err.Error()}
}

func extensionFor(path string) string {
	base := filepath.Base(path)
	if strings.HasPrefix(base, ".") && strings.Count(base, ".") == 1 {
		return "<none>"
	}
	ext := strings.ToLower(filepath.Ext(base))
	if ext == "" {
		return "<none>"
	}
	return ext
}

func markSeen(seen map[string]struct{}, path string) bool {
	key := pathKey(path)
	if _, exists := seen[key]; exists {
		return false
	}
	seen[key] = struct{}{}
	return true
}

func pathKey(path string) string {
	path = filepath.Clean(path)
	if runtime.GOOS == "windows" {
		return strings.ToLower(path)
	}
	return path
}

func isLink(info fs.FileInfo) bool {
	if info.Mode()&os.ModeSymlink != 0 {
		return true
	}
	// Windows junction 等 reparse point 在不同 Go 版本中可能不会映射为
	// ModeSymlink；通过 Sys 的字段名反射读取 FileAttributes，避免引入平台
	// 专用文件导致交叉编译失败。
	value := reflect.ValueOf(info.Sys())
	if !value.IsValid() {
		return false
	}
	if value.Kind() == reflect.Ptr {
		if value.IsNil() {
			return false
		}
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return false
	}
	field := value.FieldByName("FileAttributes")
	if !field.IsValid() || !field.CanUint() {
		return false
	}
	const fileAttributeReparsePoint = uint64(0x400)
	return field.Uint()&fileAttributeReparsePoint != 0
}
