package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"clictl/internal/service"
)

func getPrefs(t *testing.T, h http.Handler) UIPreferences {
	t.Helper()
	rec, env := do(t, h, http.MethodGet, "/api/preferences", nil)
	if rec.Code != http.StatusOK || !env.OK {
		t.Fatalf("读取偏好失败: HTTP %d, %s", rec.Code, rec.Body.String())
	}
	var prefs UIPreferences
	if err := json.Unmarshal(env.Data, &prefs); err != nil {
		t.Fatalf("解析偏好失败: %v", err)
	}
	return prefs
}

func TestPreferencesDefaults(t *testing.T) {
	h, _ := newTestServer(t)
	prefs := getPrefs(t, h)
	if prefs.ViewMode != "flat" || prefs.SortMode != "name" {
		t.Fatalf("默认偏好异常: %+v", prefs)
	}
	if prefs.CollapsedGroups == nil || len(prefs.CollapsedGroups) != 0 {
		t.Fatalf("默认折叠组应为空数组: %+v", prefs)
	}
}

func TestPreferencesPersistAndNormalize(t *testing.T) {
	h, _ := newTestServer(t)
	rec, env := do(t, h, http.MethodPost, "/api/preferences", map[string]any{
		"view_mode":        "tree",
		"sort_mode":        "recent",
		"collapsed_groups": []string{"dev", "", "dev", "release"},
	})
	if rec.Code != http.StatusOK || !env.OK {
		t.Fatalf("保存偏好失败: HTTP %d, %s", rec.Code, rec.Body.String())
	}
	prefs := getPrefs(t, h)
	if prefs.ViewMode != "tree" || prefs.SortMode != "recent" {
		t.Fatalf("偏好未持久化: %+v", prefs)
	}
	if len(prefs.CollapsedGroups) != 2 || prefs.CollapsedGroups[0] != "dev" || prefs.CollapsedGroups[1] != "release" {
		t.Fatalf("折叠组未去重/过滤: %+v", prefs.CollapsedGroups)
	}
}

func TestPreferencesInvalidValuesFallBack(t *testing.T) {
	h, _ := newTestServer(t)
	_, env := do(t, h, http.MethodPost, "/api/preferences", map[string]any{
		"view_mode": "bogus",
		"sort_mode": "bogus",
	})
	if !env.OK {
		t.Fatalf("保存偏好失败: %s", string(env.Data))
	}
	prefs := getPrefs(t, h)
	if prefs.ViewMode != "flat" || prefs.SortMode != "name" {
		t.Fatalf("非法值应回落默认: %+v", prefs)
	}
}

func TestPreferencesFileLivesBesideDB(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "clictl.db")
	svc, err := service.OpenAt(dbPath)
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	h := NewServer(svc, Meta{DBPath: dbPath}).Handler()

	if _, env := do(t, h, http.MethodPost, "/api/preferences", map[string]any{"view_mode": "tree"}); !env.OK {
		t.Fatalf("保存偏好失败: %s", string(env.Data))
	}
	if _, err := os.Stat(filepath.Join(dir, uiPreferencesFileName)); err != nil {
		t.Fatalf("偏好文件未落在数据库同目录: %v", err)
	}
}
