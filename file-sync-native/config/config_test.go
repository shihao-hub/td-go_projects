package config

import (
	"path/filepath"
	"testing"

	"file-sync-native/models"
)

func TestRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	task := models.NewSyncTask("测试", "C:/a", "D:/b", []string{"*.log"})
	if err := c.AddTask(task); err != nil {
		t.Fatal(err)
	}

	c2, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(c2.ListTasks()) != 1 {
		t.Fatalf("want 1 task, got %d", len(c2.ListTasks()))
	}
	got, err := c2.GetTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "测试" || got.SourcePath != "C:/a" || got.TargetPath != "D:/b" || len(got.IgnoreRules) != 1 || !got.Enabled {
		t.Errorf("task fields mismatch: %+v", got)
	}

	task.Name = "改名"
	if err := c2.UpdateTask(task); err != nil {
		t.Fatal(err)
	}
	c3, _ := Load(p)
	if c3.ListTasks()[0].Name != "改名" {
		t.Error("update not persisted")
	}

	if err := c3.DeleteTask(task.ID); err != nil {
		t.Fatal(err)
	}
	c4, _ := Load(p)
	if len(c4.ListTasks()) != 0 {
		t.Error("delete not persisted")
	}
	if _, err := c4.GetTask(task.ID); err != ErrTaskNotFound {
		t.Errorf("want ErrTaskNotFound, got %v", err)
	}
}
