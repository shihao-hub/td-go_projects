package sync

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"file-sync/models"
)

func Execute(ctx context.Context, sourceRoot, targetRoot string, diff *models.FileDiff, pt *ProgressTracker) []error {
	var errs []error

	copies := make([]string, 0, len(diff.Added)+len(diff.Modified))
	copies = append(copies, diff.Added...)
	copies = append(copies, diff.Modified...)
	total := len(copies) + len(diff.Deleted)
	pt.StartSync(total)

	for i, rel := range copies {
		if ctx.Err() != nil {
			pt.Fail("已取消")
			return append(errs, context.Canceled)
		}
		pt.SetProgress(i+1, total, rel)
		src := filepath.Join(sourceRoot, filepath.FromSlash(rel))
		dst := filepath.Join(targetRoot, filepath.FromSlash(rel))
		if err := copyFile(src, dst); err != nil {
			errs = append(errs, fmt.Errorf("复制 %s: %w", rel, err))
		}
	}

	for i, rel := range diff.Deleted {
		if ctx.Err() != nil {
			pt.Fail("已取消")
			return append(errs, context.Canceled)
		}
		pt.SetProgress(len(copies)+i+1, total, rel)
		if err := os.Remove(filepath.Join(targetRoot, filepath.FromSlash(rel))); err != nil && !os.IsNotExist(err) {
			errs = append(errs, fmt.Errorf("删除 %s: %w", rel, err))
		}
	}

	if ctx.Err() != nil {
		pt.Fail("已取消")
		return append(errs, context.Canceled)
	}

	removeEmptyDirs(targetRoot)

	if len(errs) > 0 {
		pt.Fail(fmt.Sprintf("%d 项操作失败，首个错误: %v", len(errs), errs[0]))
		return errs
	}
	pt.Complete()
	return nil
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".file-sync-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	ok := false
	defer func() {
		tmp.Close()
		if !ok {
			os.Remove(name)
		}
	}()

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	buf := make([]byte, 1024*1024)
	if _, err := io.CopyBuffer(tmp, in, buf); err != nil {
		return err
	}
	if si, err := os.Stat(src); err == nil {
		_ = os.Chmod(name, si.Mode().Perm())
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, dst); err != nil {
		return err
	}
	ok = true
	return nil
}

func removeEmptyDirs(root string) {
	var dirs []string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && p != root {
			dirs = append(dirs, p)
		}
		return nil
	})
	sort.Sort(sort.Reverse(sort.StringSlice(dirs)))
	for _, d := range dirs {
		_ = os.Remove(d)
	}
}
