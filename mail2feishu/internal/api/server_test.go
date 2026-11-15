package api

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail2feishu/internal/runtime"
)

const testConfigYAML = `
imap:
  username: test@qq.com
  password: imap-secret
feishu:
  webhook_url: https://open.feishu.cn/open-apis/bot/v2/hook/token
  secret: sign-secret
whitelist:
  - name: github
    sender_domains:
      - github.com
`

func newTestServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(testConfigYAML), 0o644); err != nil {
		t.Fatalf("写入测试配置失败: %v", err)
	}
	rt, err := runtime.New(runtime.Options{
		ConfigPath: configPath,
		DataDir:    dir,
		Logger:     log.New(io.Discard, "", 0),
	})
	if err != nil {
		t.Fatalf("runtime.New() 错误: %v", err)
	}
	t.Cleanup(rt.Close)
	return NewServer(rt)
}

func requestJSON(t *testing.T, handler http.Handler, method, path, body string) (int, map[string]any) {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("响应不是 JSON（HTTP %d）: %s", rec.Code, rec.Body.String())
	}
	return rec.Code, payload
}

func TestStatusEndpoint(t *testing.T) {
	server := newTestServer(t)
	status, payload := requestJSON(t, server.Handler(), http.MethodGet, "/api/status", "")
	if status != http.StatusOK || payload["ok"] != true {
		t.Fatalf("status=%d payload=%v", status, payload)
	}
	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("data 类型异常: %T", payload["data"])
	}
	if data["username"] != "test@qq.com" || data["rules"] != float64(1) {
		t.Errorf("status data = %v", data)
	}
	if data["config_path"] == "" || data["data_dir"] == "" {
		t.Errorf("路径字段缺失: %v", data)
	}
}

func TestConfigEndpointRedactsSecrets(t *testing.T) {
	server := newTestServer(t)
	status, payload := requestJSON(t, server.Handler(), http.MethodGet, "/api/config", "")
	if status != http.StatusOK || payload["ok"] != true {
		t.Fatalf("status=%d payload=%v", status, payload)
	}
	data := payload["data"].(map[string]any)
	imap := data["imap"].(map[string]any)
	if imap["password"] != "" {
		t.Errorf("密码不应回传: %v", imap["password"])
	}
	if imap["password_set"] != true {
		t.Errorf("password_set 应为 true: %v", imap)
	}
	feishu := data["feishu"].(map[string]any)
	if feishu["secret"] != "" || feishu["secret_set"] != true {
		t.Errorf("签名密钥脱敏异常: %v", feishu)
	}
	if feishu["webhook_url"] == "" {
		t.Error("webhook_url 应可编辑")
	}
}

func TestConfigUpdateRejectsInvalid(t *testing.T) {
	server := newTestServer(t)
	body := `{"imap":{"host":"imap.qq.com","port":993,"username":"u@qq.com","password":"","mailbox":"INBOX"},` +
		`"feishu":{"webhook_url":"https://open.feishu.cn/hook"},"whitelist":[{"name":"empty"}]}`
	status, payload := requestJSON(t, server.Handler(), http.MethodPut, "/api/config", body)
	if status != http.StatusBadRequest || payload["ok"] != false {
		t.Fatalf("非法配置应返回 400，got %d %v", status, payload)
	}
	errObj := payload["error"].(map[string]any)
	if errObj["code"] != "config_invalid" {
		t.Errorf("error.code = %v", errObj["code"])
	}
}

func TestTestMailEndpointWithoutSender(t *testing.T) {
	server := newTestServer(t)
	status, payload := requestJSON(t, server.Handler(), http.MethodPost, "/api/test-mail", "")
	if status != http.StatusBadRequest || payload["ok"] != false {
		t.Fatalf("未配置测试发信应返回 400，got %d %v", status, payload)
	}
	errObj := payload["error"].(map[string]any)
	if errObj["code"] != "test_sender_missing" {
		t.Errorf("error.code = %v", errObj["code"])
	}
}

func TestRecordsAndLogsEndpoints(t *testing.T) {
	server := newTestServer(t)
	status, payload := requestJSON(t, server.Handler(), http.MethodGet, "/api/records?limit=10", "")
	if status != http.StatusOK || payload["ok"] != true {
		t.Fatalf("records status=%d payload=%v", status, payload)
	}

	status, payload = requestJSON(t, server.Handler(), http.MethodGet, "/api/logs?tail=10", "")
	if status != http.StatusOK || payload["ok"] != true {
		t.Fatalf("logs status=%d payload=%v", status, payload)
	}
	lines, ok := payload["data"].(map[string]any)["lines"]
	if !ok {
		t.Fatalf("logs data 缺少 lines: %v", payload["data"])
	}
	if _, ok := lines.([]any); !ok && lines != nil {
		t.Fatalf("lines 类型异常: %T", lines)
	}
}

func TestMetaEndpoint(t *testing.T) {
	server := newTestServer(t)
	status, payload := requestJSON(t, server.Handler(), http.MethodGet, "/api/meta", "")
	if status != http.StatusOK || payload["ok"] != true {
		t.Fatalf("meta status=%d payload=%v", status, payload)
	}
	data := payload["data"].(map[string]any)
	if data["version"] == "" || data["mode"] == "" {
		t.Errorf("meta data = %v", data)
	}
}

func TestStaticAssetsServed(t *testing.T) {
	server := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("首页 HTTP %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "mail2feishu") {
		t.Error("首页内容异常")
	}
}
