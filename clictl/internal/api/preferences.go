package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// uiPreferencesFileName UI 偏好文件名，与数据库同通道目录落盘。
// GUI 每次以随机端口启动，localStorage 的 origin 会变化，因此偏好必须服务端持久化。
const uiPreferencesFileName = "ui_preferences.json"

// UIPreferences WebUI 视图偏好。
type UIPreferences struct {
	ViewMode    string `json:"view_mode"`    // flat | tabs，默认 flat；旧值 tree 归一化为 tabs
	SortMode    string `json:"sort_mode"`    // name | recent | launches | added
	ActiveGroup string `json:"active_group"` // tabs 视图当前选中的分组名，空串 = 全部
}

func defaultUIPreferences() UIPreferences {
	return UIPreferences{
		ViewMode:    "flat",
		SortMode:    "name",
		ActiveGroup: "",
	}
}

// normalizePreferences 收口非法值，保证前端拿到的永远是受支持枚举。
func normalizePreferences(p UIPreferences) UIPreferences {
	switch p.ViewMode {
	case "tabs":
	case "tree":
		p.ViewMode = "tabs" // 旧版树状视图已由分组 tab 取代，静默迁移
	default:
		p.ViewMode = "flat"
	}
	switch p.SortMode {
	case "name", "recent", "launches", "added":
	default:
		p.SortMode = "name"
	}
	p.ActiveGroup = strings.TrimSpace(p.ActiveGroup)
	return p
}

// preferencesPath 偏好文件与数据库同目录（dev / prod 通道天然隔离）。
func (s *Server) preferencesPath() string {
	if s.meta.DBPath == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(s.meta.DBPath), uiPreferencesFileName)
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
	return normalizePreferences(loaded)
}

func (s *Server) savePreferences(prefs UIPreferences) error {
	path := s.preferencesPath()
	if path == "" {
		return errors.New("偏好路径未初始化（DBPath 为空）")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(normalizePreferences(prefs), "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func (s *Server) handleGetPreferences(w http.ResponseWriter, r *http.Request) {
	writeOK(w, s.loadPreferences())
}

func (s *Server) handlePostPreferences(w http.ResponseWriter, r *http.Request) {
	var prefs UIPreferences
	if err := decodeJSON(r, &prefs); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_args", err.Error())
		return
	}
	if err := s.savePreferences(prefs); err != nil {
		writeErr(w, http.StatusInternalServerError, "io_error", err.Error())
		return
	}
	writeOK(w, s.loadPreferences())
}
