package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// /api/meta 只读 meta 不触库，可安全用于 Host 中间件行为验证
func TestLocalHostGuard(t *testing.T) {
	server := NewServer(nil, Meta{})
	handler := server.Handler()

	cases := []struct {
		name string
		host string
		want int
	}{
		{"回环显式端口", "127.0.0.1:52301", http.StatusOK},
		{"localhost", "localhost:9000", http.StatusOK},
		{"IPv6 回环", "[::1]:9000", http.StatusOK},
		{"非本机域名", "evil.com", http.StatusForbidden},
		{"伪装后缀域名", "127.0.0.1.evil.com:80", http.StatusForbidden},
		{"同义盘旋名", "localhost.evil.com", http.StatusForbidden},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "http://"+tc.host+"/api/meta", nil)
			req.Host = tc.host
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Errorf("Host=%q 状态码 = %d, want %d", tc.host, rec.Code, tc.want)
			}
		})
	}
}
