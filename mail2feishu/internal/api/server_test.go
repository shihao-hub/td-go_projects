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

const testHost = "127.0.0.1:8080"

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

func newRequest(method, path, body string) *http.Request {
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Host = testHost
	return req
}

func requestJSON(t *testing.T, server *Server, method, path, body string) (int, map[string]any) {
	t.Helper()
	req := newRequest(method, path, body)
	req.Header.Set(tokenHeader, server.Token())
	return serveJSON(t, server, req)
}

func serveJSON(t *testing.T, server *Server, req *http.Request) (int, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("响应不是 JSON（HTTP %d）: %s", rec.Code, rec.Body.String())
	}
	return rec.Code, payload
}

func TestStatusEndpoint(t *testing.T) {
	server := newTestServer(t)
	status, payload := requestJSON(t, server, http.MethodGet, "/api/status", "")
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
	status, payload := requestJSON(t, server, http.MethodGet, "/api/config", "")
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
	// Webhook 路径 token 本身就是投递凭证，不能完整回传。
	if feishu["webhook_url"] != "" {
		t.Errorf("Webhook 地址不应完整回传: %v", feishu["webhook_url"])
	}
	if feishu["webhook_set"] != true {
		t.Errorf("webhook_set 应为 true: %v", feishu)
	}
	hint, _ := feishu["webhook_hint"].(string)
	if !strings.Contains(hint, "open.feishu.cn") || strings.Contains(hint, "bot/v2/hook/token") {
		t.Errorf("Webhook 提示应保留主机并隐藏完整路径: %q", hint)
	}
}

func TestConfigUpdateRejectsInvalid(t *testing.T) {
	server := newTestServer(t)
	body := `{"imap":{"host":"imap.qq.com","port":993,"username":"u@qq.com","password":"","mailbox":"INBOX"},` +
		`"feishu":{"webhook_url":"https://open.feishu.cn/hook"},"whitelist":[{"name":"empty"}]}`
	status, payload := requestJSON(t, server, http.MethodPut, "/api/config", body)
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
	status, payload := requestJSON(t, server, http.MethodPost, "/api/test-mail", "")
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
	status, payload := requestJSON(t, server, http.MethodGet, "/api/records?limit=10", "")
	if status != http.StatusOK || payload["ok"] != true {
		t.Fatalf("records status=%d payload=%v", status, payload)
	}

	status, payload = requestJSON(t, server, http.MethodGet, "/api/logs?tail=10", "")
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
	status, payload := requestJSON(t, server, http.MethodGet, "/api/meta", "")
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
	req := newRequest(http.MethodGet, "/", "")
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("首页 HTTP %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "mail2feishu") {
		t.Error("首页内容异常")
	}
}

// TestAPIRequiresToken 锁定「本机其他进程不能直接调用控制接口」。
func TestAPIRequiresToken(t *testing.T) {
	server := newTestServer(t)
	cases := []struct{ method, path string }{
		{http.MethodGet, "/api/status"},
		{http.MethodGet, "/api/config"},
		{http.MethodGet, "/api/records"},
		{http.MethodPost, "/api/test-webhook"},
		{http.MethodPost, "/api/test-mail"},
		{http.MethodPost, "/api/reconnect"},
		{http.MethodPut, "/api/config"},
	}
	for _, tc := range cases {
		req := newRequest(tc.method, tc.path, "")
		status, payload := serveJSON(t, server, req)
		if status != http.StatusUnauthorized || payload["ok"] != false {
			t.Errorf("%s %s 应返回 401，got %d %v", tc.method, tc.path, status, payload)
		}
	}

	req := newRequest(http.MethodGet, "/api/status", "")
	req.Header.Set(tokenHeader, "wrong-token")
	if status, _ := serveJSON(t, server, req); status != http.StatusUnauthorized {
		t.Errorf("错误令牌应返回 401，got %d", status)
	}
}

// TestAPIRejectsForeignHostAndOrigin 防御 DNS rebinding 与跨站请求。
func TestAPIRejectsForeignHostAndOrigin(t *testing.T) {
	server := newTestServer(t)

	req := newRequest(http.MethodGet, "/api/status", "")
	req.Host = "evil.example.com"
	req.Header.Set(tokenHeader, server.Token())
	if status, payload := serveJSON(t, server, req); status != http.StatusForbidden {
		t.Errorf("非回环 Host 应返回 403，got %d %v", status, payload)
	}

	req = newRequest(http.MethodPost, "/api/reconnect", "")
	req.Header.Set(tokenHeader, server.Token())
	req.Header.Set("Origin", "https://evil.example.com")
	if status, payload := serveJSON(t, server, req); status != http.StatusForbidden {
		t.Errorf("跨站 Origin 应返回 403，got %d %v", status, payload)
	}

	// 同源请求应放行。
	req = newRequest(http.MethodGet, "/api/status", "")
	req.Header.Set(tokenHeader, server.Token())
	req.Header.Set("Origin", "http://"+testHost)
	if status, payload := serveJSON(t, server, req); status != http.StatusOK {
		t.Errorf("同源请求应通过，got %d %v", status, payload)
	}
}
