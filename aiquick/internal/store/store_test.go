package store

import (
	"strings"
	"testing"

	"aiquick/internal/api"
)

func TestOpenSeedsDefaults(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg := s.Config()
	if cfg.BaseURL != "https://open.bigmodel.cn/api/paas/v4" {
		t.Fatalf("default baseURL wrong: %s", cfg.BaseURL)
	}
	if cfg.Model == "" || cfg.APIKey != "" {
		t.Fatalf("default model/key wrong: %+v", cfg)
	}
	ps := s.Presets()
	if len(ps) != 3 {
		t.Fatalf("want 3 seed presets, got %d", len(ps))
	}
	for _, p := range ps {
		if p.ID == "" || p.Name == "" || p.System == "" {
			t.Fatalf("bad seed preset: %+v", p)
		}
	}
	// 种子应落盘
	s2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(s2.Presets()) != 3 {
		t.Fatal("presets.json should be seeded on disk")
	}
}

func TestConfigPersist(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := api.Config{BaseURL: "http://localhost:1", APIKey: "sk-x", Model: "m1"}
	if err := s.SetConfig(want); err != nil {
		t.Fatal(err)
	}
	s2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s2.Config() != want {
		t.Fatalf("config not persisted: %+v", s2.Config())
	}
}

func TestPresetSaveNewAndUpdateAndDelete(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	created, err := s.SavePreset(api.Preset{Name: "总结", System: "总结输入"})
	if err != nil {
		t.Fatal(err)
	}
	if len(created.ID) != 8 {
		t.Fatalf("generated id should be 8 hex chars: %q", created.ID)
	}
	if got := s.Presets(); len(got) != 4 {
		t.Fatalf("want 4 presets, got %d", len(got))
	}

	created.Name = "总结改"
	updated, err := s.SavePreset(created)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "总结改" {
		t.Fatalf("update lost: %+v", updated)
	}
	if got := s.Presets(); len(got) != 4 {
		t.Fatalf("update should not append, got %d", len(got))
	}

	if err := s.DeletePreset(created.ID); err != nil {
		t.Fatal(err)
	}
	if got := s.Presets(); len(got) != 3 {
		t.Fatalf("delete failed, got %d", len(got))
	}
	if err := s.DeletePreset(created.ID); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("delete missing should fail, got %v", err)
	}
}

func TestPresetsCopyIsolated(t *testing.T) {
	s, _ := Open(t.TempDir())
	ps := s.Presets()
	ps[0].Name = "hacked"
	if s.Presets()[0].Name == "hacked" {
		t.Fatal("Presets() should return a copy")
	}
}
