package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"deepseeksearch/internal/research"
	"deepseeksearch/internal/storage"
)

func TestResearchHTTPWorkflow(t *testing.T) {
	root, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	root = filepath.Join(root, "language_projects", "deepseeksearch", "dev", "tests")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp(root, "http-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	store, err := storage.NewStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/chat/completions" {
			_, _ = w.Write([]byte(`{"choices":[{"finish_reason":"stop","message":{"content":" "}}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"results":[{"title":"证据","url":"https://example.com/a","snippet":"资料","summary":"摘要","rerank_score":0.8}]}`))
	}))
	defer upstream.Close()
	if err := store.SaveConfig(storage.AppConfig{APIKey: "test-key", BaseURL: upstream.URL, AIModel: "deepseek-flash"}); err != nil {
		t.Fatal(err)
	}
	handler := NewServer(store, Meta{}).Handler()
	call := func(method, path, body, contentType string, status int) research.Session {
		t.Helper()
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		req.Host = "127.0.0.1"
		req.Header.Set("Content-Type", contentType)
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != status {
			t.Fatalf("%s %s: %d %s", method, path, res.Code, res.Body.String())
		}
		var out struct {
			Data research.Session `json:"data"`
		}
		_ = json.Unmarshal(res.Body.Bytes(), &out)
		return out.Data
	}
	call("POST", "/api/research", `{"question":"问题"}`, "text/plain", 400)
	call("POST", "/api/research", `{"question":"问题","extra":true}`, "application/json", 400)
	v := call("POST", "/api/research", `{"question":"技术选型"}`, "application/json", 200)
	if v.ID == "" {
		t.Fatal("研究未创建")
	}
	prefix := "/api/research/" + v.ID
	call("POST", prefix+"/run", `{}`, "application/json", 200)
	v = call("GET", prefix, "", "application/json", 200)
	if len(v.Results) != 1 {
		t.Fatal("已保存结果未恢复")
	}
	call("POST", prefix+"/feedback", `{"kind":"need_examples","text":"更多案例"}`, "application/json", 200)
	call("POST", prefix+"/suggest", `{}`, "application/json", 502)
	v = call("GET", prefix, "", "application/json", 200)
	if len(v.Feedback) != 1 || len(v.Results) != 1 {
		t.Fatal("模型错误丢失记录")
	}
	call("POST", prefix+"/run", `{}`, "application/json", 400)
}
