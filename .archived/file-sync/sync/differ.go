package sync

import (
	"sort"

	"file-sync/models"
)

func ComputeDiff(source, target *models.FileTree) *models.FileDiff {
	d := &models.FileDiff{Added: []string{}, Modified: []string{}, Deleted: []string{}}
	if source != nil {
		for p, sf := range source.Files {
			if sf.IsDir {
				continue
			}
			tf, ok := fileIn(target, p)
			if !ok {
				d.Added = append(d.Added, p)
				continue
			}
			if sf.Hash != tf.Hash {
				d.Modified = append(d.Modified, p)
			}
		}
	}
	if target != nil {
		for p, tf := range target.Files {
			if tf.IsDir {
				continue
			}
			if source != nil {
				if sf, ok := source.Files[p]; ok && !sf.IsDir {
					continue
				}
			}
			d.Deleted = append(d.Deleted, p)
		}
	}
	sort.Strings(d.Added)
	sort.Strings(d.Modified)
	sort.Strings(d.Deleted)
	return d
}

func fileIn(t *models.FileTree, p string) (*models.FileInfo, bool) {
	if t == nil {
		return nil, false
	}
	f, ok := t.Files[p]
	if !ok || f.IsDir {
		return nil, false
	}
	return f, true
}

func AllFiles(t *models.FileTree) []string {
	out := []string{}
	if t == nil {
		return out
	}
	for p, f := range t.Files {
		if !f.IsDir {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}
