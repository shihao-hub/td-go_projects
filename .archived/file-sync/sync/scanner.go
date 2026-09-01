package sync

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"file-sync/ignore"
	"file-sync/models"
)

func ScanDirectory(root string, m *ignore.Matcher, onFile func(relPath string)) (*models.FileTree, error) {
	root = filepath.Clean(root)
	tree := &models.FileTree{Files: make(map[string]*models.FileInfo)}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			return nil
		}
		if rel == "." {
			if err != nil {
				return err
			}
			return nil
		}
		if err != nil {
			tree.Errors = append(tree.Errors, p+": "+err.Error())
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			tree.Errors = append(tree.Errors, "跳过符号链接: "+p)
			return nil
		}
		relSlash := filepath.ToSlash(rel)
		if m != nil && m.Match(relSlash, d.IsDir()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			tree.Errors = append(tree.Errors, p+": "+ierr.Error())
			return nil
		}
		fi := &models.FileInfo{
			Path:    relSlash,
			Size:    info.Size(),
			ModTime: info.ModTime(),
			IsDir:   d.IsDir(),
		}
		if !d.IsDir() {
			h, herr := computeHash(p)
			if herr != nil {
				tree.Errors = append(tree.Errors, p+": "+herr.Error())
				return nil
			}
			fi.Hash = h
		}
		tree.Files[relSlash] = fi
		if !d.IsDir() && onFile != nil {
			onFile(relSlash)
		}
		return nil
	})
	if err != nil {
		return tree, err
	}
	return tree, nil
}

func computeHash(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	buf := make([]byte, 1024*1024)
	if _, err := io.CopyBuffer(h, f, buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
