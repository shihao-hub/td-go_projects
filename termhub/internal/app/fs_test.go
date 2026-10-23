// fs_test.go：目录浏览 API 的行为断言。
package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func listDirs(t *testing.T, path string) (*httptest.ResponseRecorder, APIResponse) {
	t.Helper()
	srv := NewServer("", nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/fs/list?path="+url.QueryEscape(path), nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	var resp APIResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	return rec, resp
}

// TestListDirs_OnlyDirectoriesSorted 列出目录：只含子目录、名称不区分大小写排序。
func TestListDirs_OnlyDirectoriesSorted(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"beta", "Alpha", "gamma"} {
		if err := os.Mkdir(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	rec, resp := listDirs(t, dir)
	if rec.Code != http.StatusOK || !resp.OK {
		t.Fatalf("status=%d resp=%+v", rec.Code, resp)
	}

	raw, err := json.Marshal(resp.Data)
	if err != nil {
		t.Fatal(err)
	}
	var listing fsListing
	if err := json.Unmarshal(raw, &listing); err != nil {
		t.Fatal(err)
	}

	if listing.Path != dir {
		t.Fatalf("path = %q, want %q", listing.Path, dir)
	}
	if listing.Parent != filepath.Dir(dir) {
		t.Fatalf("parent = %q, want %q", listing.Parent, filepath.Dir(dir))
	}
	want := []string{"Alpha", "beta", "gamma"}
	if len(listing.Dirs) != len(want) {
		t.Fatalf("dirs = %+v, want %v", listing.Dirs, want)
	}
	for i, name := range want {
		if listing.Dirs[i].Name != name {
			t.Fatalf("dirs[%d] = %q, want %q", i, listing.Dirs[i].Name, name)
		}
		if listing.Dirs[i].Path != filepath.Join(dir, name) {
			t.Fatalf("dirs[%d].path = %q", i, listing.Dirs[i].Path)
		}
	}
}

// TestListDirs_MissingDirectory 不存在的目录返回 400。
func TestListDirs_MissingDirectory(t *testing.T) {
	rec, resp := listDirs(t, filepath.Join(t.TempDir(), "not-there"))
	if rec.Code != http.StatusBadRequest || resp.OK {
		t.Fatalf("status=%d resp=%+v", rec.Code, resp)
	}
}
