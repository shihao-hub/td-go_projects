package engine

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// copyFile 以 临时文件+rename 的方式原子复制，并同步源文件的修改时间。
// 修改时间同步是快速比对（size+mtime）的前提：复制出的目标文件
// mtime 等于源文件，下次扫描即可零 I/O 跳过。
func copyFile(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".fsync-*")
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
	if err := os.Chmod(name, srcInfo.Mode().Perm()); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, dst); err != nil {
		return err
	}
	ok = true
	if err := os.Chtimes(dst, time.Now(), srcInfo.ModTime()); err != nil {
		return err
	}
	return nil
}

// deleteFiles 逐个删除目标侧多余文件，ctx 取消即停。
func deleteFiles(ctx context.Context, root string, rels []string, onDelete func(done, total int, rel string)) []error {
	var errs []error
	for i, rel := range rels {
		if ctx.Err() != nil {
			errs = append(errs, context.Canceled)
			return errs
		}
		onDelete(i+1, len(rels), rel)
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			errs = append(errs, fmt.Errorf("删除 %s: %w", rel, err))
		}
	}
	return errs
}

// removeEmptyDirs 自底向上清掉目标侧的空目录。
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
