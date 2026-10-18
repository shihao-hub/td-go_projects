package engine

import (
	"testing"
)

func TestListRegistryItems(t *testing.T) {
	items, err := ListRegistryItems()
	if err != nil {
		t.Fatalf("ListRegistryItems failed: %v", err)
	}
	t.Logf("Found %d registry startup items:", len(items))
	for _, it := range items {
		t.Logf("[%s] %s -> Exe: %s, Enabled: %v, FileExists: %v, Publisher: %s",
			it.Source, it.Name, it.ExePath, it.IsEnabled, it.FileExists, it.Publisher)
	}
}