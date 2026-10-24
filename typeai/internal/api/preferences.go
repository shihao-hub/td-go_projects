package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
)

const uiPreferencesFileName = "ui_preferences.json"

// UIPreferences 保存客户端 UI 视图偏好，持久化在用户数据目录。
type UIPreferences struct {
	ViewMode        string   `json:"view_mode"`        // "tree" | "list"
	CollapsedGroups []string `json:"collapsed_groups"` // 折叠的分组 id 列表
}

func defaultUIPreferences() UIPreferences {
	return UIPreferences{
		ViewMode:        "tree",
		CollapsedGroups: []string{},
	}
}

func (s *Server) preferencesPath() string {
	if s.dataDir == "" {
		return ""
	}
	return filepath.Join(s.dataDir, uiPreferencesFileName)
}

func (s *Server) loadPreferences() UIPreferences {
	prefs := defaultUIPreferences()
	path := s.preferencesPath()
	if path == "" {
		return prefs
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return prefs
	}
	var loaded UIPreferences
	if err := json.Unmarshal(data, &loaded); err != nil {
		return prefs
	}
	if loaded.ViewMode != "tree" && loaded.ViewMode != "list" {
		loaded.ViewMode = "tree"
	}
	if loaded.CollapsedGroups == nil {
		loaded.CollapsedGroups = []string{}
	}
	return loaded
}

func (s *Server) savePreferences(prefs UIPreferences) error {
	path := s.preferencesPath()
	if path == "" {
		return errors.New("dataDir 未设置")
	}
	if prefs.ViewMode != "tree" && prefs.ViewMode != "list" {
		prefs.ViewMode = "tree"
	}
	if prefs.CollapsedGroups == nil {
		prefs.CollapsedGroups = []string{}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(prefs, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func (s *Server) handleGetPreferences(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.loadPreferences())
}

func (s *Server) handlePostPreferences(w http.ResponseWriter, r *http.Request) {
	var prefs UIPreferences
	if !decodeJSON(w, r, 64*1024, &prefs) {
		return
	}
	if err := s.savePreferences(prefs); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "io_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.loadPreferences())
}
