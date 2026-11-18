package deepseek

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSuggestResponseValidation(t *testing.T) {
	cases := []struct {
		name, content, finish string
		valid                 bool
	}{
		{"成功", `{"suggestions":[{"query":"Go 案例","reason":"工程实践"}]}`, "stop", true},
		{"空白", "  ", "stop", false}, {"空数组", `{"suggestions":[]}`, "stop", false},
		{"截断", `{"suggestions":[{"query":"词","reason":"目的"}]}`, "length", false},
		{"空词", `{"suggestions":[{"query":" "}]}`, "stop", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/chat/completions" {
					t.Error("调用了错误接口")
				}
				var body map[string]interface{}
				if json.NewDecoder(r.Body).Decode(&body) != nil {
					t.Error("请求无效")
				}
				if body["model"] != "test-model" {
					t.Error("模型设置丢失")
				}
				_ = json.NewEncoder(w).Encode(map[string]interface{}{"choices": []interface{}{map[string]interface{}{"finish_reason": tc.finish, "message": map[string]string{"content": tc.content}}}})
			}))
			defer server.Close()
			_, err := NewClient("test-key", server.URL).Suggest(context.Background(), "test-model", "{}")
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v err=%v", tc.valid, err)
			}
		})
	}
}
