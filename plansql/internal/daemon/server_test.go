package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// writeProject 在临时目录里落一个最小 plans_status.sql（支持多条登记）。
func writeProject(t *testing.T, root string, relPaths ...string) {
	t.Helper()
	sql := ""
	for _, relPath := range relPaths {
		sql += "INSERT INTO plan_spec_status (type, path, status)\n" +
			"VALUES ('plan', '" + relPath + "', '{\"state\":\"completed\"}');\n"
	}
	if err := os.WriteFile(filepath.Join(root, "plans_status.sql"), []byte(sql), 0o644); err != nil {
		t.Fatalf("写入 SQL 失败: %v", err)
	}
}

func countItems(t *testing.T, srv *Server) int {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/items", nil)
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("items 状态码 = %d, body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		OK   bool  `json:"ok"`
		Data []any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if !resp.OK {
		t.Fatalf("响应 ok=false: %s", rec.Body.String())
	}
	return len(resp.Data)
}

func TestReconfigureSwitchesProjectRoot(t *testing.T) {
	rootA := t.TempDir()
	rootB := t.TempDir()
	writeProject(t, rootA, "docs/plans/a.md")
	writeProject(t, rootB, "docs/plans/b.md", "docs/specs/b.md")

	srv, err := NewServer(Config{
		RootDir:    rootA,
		SQLFile:    filepath.Join(rootA, "plans_status.sql"),
		NoAddrFile: true,
	})
	if err != nil {
		t.Fatalf("NewServer 失败: %v", err)
	}
	defer srv.Stop()

	if got := countItems(t, srv); got != 1 {
		t.Fatalf("项目 A 应有 1 条记录，实际 %d", got)
	}

	if err := srv.Reconfigure(rootB, ""); err != nil {
		t.Fatalf("切换项目根失败: %v", err)
	}
	if got := countItems(t, srv); got != 2 {
		t.Fatalf("项目 B 应有 2 条记录，实际 %d", got)
	}

	root, sqlFile := srv.Project()
	if root != rootB || sqlFile != filepath.Join(rootB, "plans_status.sql") {
		t.Fatalf("项目根未更新: root=%s sql=%s", root, sqlFile)
	}
}

func TestReconfigureRejectsMissingDirectory(t *testing.T) {
	root := t.TempDir()
	writeProject(t, root, "docs/plans/a.md")

	srv, err := NewServer(Config{
		RootDir:    root,
		SQLFile:    filepath.Join(root, "plans_status.sql"),
		NoAddrFile: true,
	})
	if err != nil {
		t.Fatalf("NewServer 失败: %v", err)
	}
	defer srv.Stop()

	if err := srv.Reconfigure(filepath.Join(root, "not-exists"), ""); err == nil {
		t.Fatal("切换到不存在的目录应失败")
	}
	if got := countItems(t, srv); got != 1 {
		t.Fatalf("失败的切换不应破坏旧状态，实际 %d 条", got)
	}
}
