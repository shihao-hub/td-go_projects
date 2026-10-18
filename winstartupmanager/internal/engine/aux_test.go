package engine

import (
	"testing"
)

func TestListFolderItems(t *testing.T) {
	items, err := ListFolderItems()
	if err != nil {
		t.Fatalf("ListFolderItems failed: %v", err)
	}
	t.Logf("Found %d startup folder items:", len(items))
	for _, it := range items {
		t.Logf("[%s] %s -> Exe: %s, Publisher: %s, FileExists: %v", it.Source, it.Name, it.ExePath, it.Publisher, it.FileExists)
	}
}

func TestListScheduledTasks(t *testing.T) {
	items, err := ListScheduledTasks()
	if err != nil {
		t.Fatalf("ListScheduledTasks failed: %v", err)
	}
	t.Logf("Found %d scheduled startup tasks:", len(items))
	for _, it := range items {
		t.Logf("[Task] %s -> Exe: %s, Publisher: %s, Enabled: %v", it.Name, it.ExePath, it.Publisher, it.IsEnabled)
	}
}