package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"clictl/internal/service"
)

func newTestServer(t *testing.T) (http.Handler, *service.Service) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "clictl.db")
	svc, err := service.OpenAt(dbPath)
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	return NewServer(svc, Meta{Channel: "dev", Version: "test", DBPath: dbPath}).Handler(), svc
}

// fakeExe 造一个可注册的 .exe（内容不重要，add 只做路径与后缀校验）。
func fakeExe(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake.exe")
	if err := os.WriteFile(path, []byte("MZ"), 0o644); err != nil {
		t.Fatalf("写入假 exe 失败: %v", err)
	}
	return path
}

type envelope struct {
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func do(t *testing.T, h http.Handler, method, path string, body any) (*httptest.ResponseRecorder, envelope) {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("序列化请求体失败: %v", err)
		}
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var env envelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("%s %s 返回非 JSON: %q", method, path, rec.Body.String())
	}
	return rec, env
}

func TestRegisterListInfoRemove(t *testing.T) {
	h, _ := newTestServer(t)
	exe := fakeExe(t)

	rec, env := do(t, h, http.MethodPost, "/api/tools", map[string]any{
		"path": exe,
		"desc": "测试工具",
		"meta": map[string]any{"source": "test", "tags": []string{"Dev", "dev"}},
	})
	if rec.Code != http.StatusOK || !env.OK {
		t.Fatalf("注册失败: HTTP %d, %s", rec.Code, rec.Body.String())
	}

	var tool struct {
		Name string          `json:"name"`
		Meta json.RawMessage `json:"meta"`
	}
	if err := json.Unmarshal(env.Data, &tool); err != nil {
		t.Fatalf("解析注册结果失败: %v", err)
	}
	if tool.Name != "fake" {
		t.Fatalf("默认 name = %q, want fake", tool.Name)
	}

	_, listEnv := do(t, h, http.MethodGet, "/api/tools", nil)
	var tools []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(listEnv.Data, &tools); err != nil {
		t.Fatalf("解析列表失败: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "fake" {
		t.Fatalf("列表结果异常: %s", string(listEnv.Data))
	}

	_, infoEnv := do(t, h, http.MethodGet, "/api/tools/fake", nil)
	if !infoEnv.OK {
		t.Fatalf("info 失败: %s", string(infoEnv.Data))
	}

	rec, env = do(t, h, http.MethodDelete, "/api/tools/fake", nil)
	if rec.Code != http.StatusOK || !env.OK {
		t.Fatalf("删除失败: HTTP %d, %s", rec.Code, rec.Body.String())
	}
	if rec, env = do(t, h, http.MethodGet, "/api/tools/fake", nil); rec.Code != http.StatusNotFound || env.OK {
		t.Fatalf("删除后 info 应 404，实际 HTTP %d", rec.Code)
	}
}

func TestMetaValidationSurfacesAsBadRequest(t *testing.T) {
	h, _ := newTestServer(t)
	exe := fakeExe(t)

	rec, env := do(t, h, http.MethodPost, "/api/tools", map[string]any{
		"path": exe,
		"meta": map[string]any{"unknown": true},
	})
	if rec.Code != http.StatusBadRequest || env.OK {
		t.Fatalf("白名单外 key 应 400，实际 HTTP %d: %s", rec.Code, rec.Body.String())
	}
	if env.Error == nil || env.Error.Code != "meta_unknown_key" {
		t.Fatalf("错误码 = %+v, want meta_unknown_key", env.Error)
	}
}

func TestMetaAcceptsGroup(t *testing.T) {
	h, _ := newTestServer(t)
	exe := fakeExe(t)
	do(t, h, http.MethodPost, "/api/tools", map[string]any{
		"path": exe,
		"name": "demo",
		"meta": map[string]any{"group": "  Dev  "},
	})

	_, env := do(t, h, http.MethodGet, "/api/tools/demo", nil)
	var info struct {
		Tool struct {
			Meta map[string]any `json:"meta"`
		} `json:"tool"`
	}
	if err := json.Unmarshal(env.Data, &info); err != nil {
		t.Fatalf("解析 info 失败: %v", err)
	}
	if info.Tool.Meta["group"] != "Dev" {
		t.Fatalf("group 应裁剪空白后保存为 Dev，实际 %+v", info.Tool.Meta)
	}
}

func TestSetMetaAndClear(t *testing.T) {
	h, _ := newTestServer(t)
	exe := fakeExe(t)
	do(t, h, http.MethodPost, "/api/tools", map[string]any{"path": exe, "name": "demo"})

	_, env := do(t, h, http.MethodPut, "/api/tools/demo/meta", map[string]any{
		"meta": map[string]any{"source": "npm"},
	})
	if !env.OK {
		t.Fatalf("设置 meta 失败: %s", string(env.Data))
	}

	rec, env := do(t, h, http.MethodPut, "/api/tools/demo/meta", map[string]any{"meta": map[string]any{}})
	if rec.Code != http.StatusOK || !env.OK {
		t.Fatalf("清空 meta 失败: HTTP %d, %s", rec.Code, rec.Body.String())
	}
	var tool struct {
		Meta json.RawMessage `json:"meta"`
	}
	if err := json.Unmarshal(env.Data, &tool); err != nil {
		t.Fatalf("解析结果失败: %v", err)
	}
	if len(tool.Meta) != 0 {
		t.Fatalf("清空后 meta 应为空，实际 %s", string(tool.Meta))
	}
}

func TestFSBrowseListsDirsAndExe(t *testing.T) {
	h, _ := newTestServer(t)
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app.exe"), []byte("MZ"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, env := do(t, h, http.MethodGet, "/api/fs?dir="+dir, nil)
	var res struct {
		Dir     string `json:"dir"`
		Entries []struct {
			Name  string `json:"name"`
			IsDir bool   `json:"isDir"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(env.Data, &res); err != nil {
		t.Fatalf("解析目录结果失败: %v", err)
	}
	if len(res.Entries) != 2 {
		t.Fatalf("应只返回目录与 .exe，实际 %+v", res.Entries)
	}
	if !res.Entries[0].IsDir || res.Entries[0].Name != "sub" {
		t.Fatalf("目录应排在前面: %+v", res.Entries)
	}
	if res.Entries[1].Name != "app.exe" {
		t.Fatalf("第二个条目应为 app.exe: %+v", res.Entries)
	}
}

func TestStartRejectsUnknownMode(t *testing.T) {
	h, _ := newTestServer(t)
	exe := fakeExe(t)
	do(t, h, http.MethodPost, "/api/tools", map[string]any{"path": exe, "name": "demo"})

	rec, env := do(t, h, http.MethodPost, "/api/tools/demo/start", map[string]any{"mode": "bogus"})
	if rec.Code != http.StatusBadRequest || env.OK {
		t.Fatalf("非法 mode 应 400，实际 HTTP %d: %s", rec.Code, rec.Body.String())
	}
	if env.Error == nil || env.Error.Code != "bad_args" {
		t.Fatalf("错误码 = %+v, want bad_args", env.Error)
	}
}

func TestInfoReportsConsoleFlag(t *testing.T) {
	h, _ := newTestServer(t)
	exe := fakeExe(t) // 内容不是合法 PE，应安全回落为窗口程序
	do(t, h, http.MethodPost, "/api/tools", map[string]any{"path": exe, "name": "demo"})

	_, env := do(t, h, http.MethodGet, "/api/tools/demo", nil)
	var info struct {
		Console bool `json:"console"`
	}
	if err := json.Unmarshal(env.Data, &info); err != nil {
		t.Fatalf("解析 info 失败: %v", err)
	}
	if info.Console {
		t.Fatal("非法 PE 应回落为非 console")
	}
}

func TestMetaEndpoint(t *testing.T) {
	h, _ := newTestServer(t)
	_, env := do(t, h, http.MethodGet, "/api/meta", nil)
	var meta struct {
		Channel string `json:"channel"`
		Version string `json:"version"`
		Tools   int    `json:"tools"`
	}
	if err := json.Unmarshal(env.Data, &meta); err != nil {
		t.Fatalf("解析 meta 失败: %v", err)
	}
	if meta.Channel != "dev" || meta.Version != "test" {
		t.Fatalf("meta 字段异常: %+v", meta)
	}
}
