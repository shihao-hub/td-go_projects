package sync

import (
	"reflect"
	"testing"

	"file-sync/models"
)

func tree(files map[string]*models.FileInfo) *models.FileTree {
	return &models.FileTree{Files: files}
}

func TestComputeDiff(t *testing.T) {
	src := tree(map[string]*models.FileInfo{
		"same.txt":   {Path: "same.txt", Hash: "h1"},
		"changed.txt": {Path: "changed.txt", Hash: "h2"},
		"new.txt":    {Path: "new.txt", Hash: "h3"},
		"adir":       {Path: "adir", IsDir: true},
	})
	dst := tree(map[string]*models.FileInfo{
		"same.txt":    {Path: "same.txt", Hash: "h1"},
		"changed.txt": {Path: "changed.txt", Hash: "old"},
		"stale.txt":   {Path: "stale.txt", Hash: "h9"},
		"bdir":        {Path: "bdir", IsDir: true},
	})

	d := ComputeDiff(src, dst)
	if !reflect.DeepEqual(d.Added, []string{"new.txt"}) {
		t.Errorf("Added = %v", d.Added)
	}
	if !reflect.DeepEqual(d.Modified, []string{"changed.txt"}) {
		t.Errorf("Modified = %v", d.Modified)
	}
	if !reflect.DeepEqual(d.Deleted, []string{"stale.txt"}) {
		t.Errorf("Deleted = %v", d.Deleted)
	}
}

func TestComputeDiffEmptyTarget(t *testing.T) {
	src := tree(map[string]*models.FileInfo{"a.txt": {Path: "a.txt", Hash: "x"}})
	d := ComputeDiff(src, nil)
	if len(d.Added) != 1 || len(d.Modified) != 0 || len(d.Deleted) != 0 {
		t.Errorf("diff = %+v", d)
	}
	d = ComputeDiff(nil, src)
	if len(d.Deleted) != 1 {
		t.Errorf("diff = %+v", d)
	}
}
