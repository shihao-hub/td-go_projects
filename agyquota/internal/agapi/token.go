// Package agapi 封装 Antigravity / Cloud Code 的凭据与配额接口调用
// （基础设施层：只做文件读取与 HTTP，不含业务规则）。
// 凭据文件只读，绝不回写；本进程除网络缓存外零落盘。
package agapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// 默认凭据路径：Antigravity / Zed ACP antigravity agent 落盘的 Google OAuth 凭据。
const defaultTokenRelPath = ".gemini/antigravity-acp/acp_token.json"

// DefaultTokenPath 返回凭据文件默认路径（优先 USERPROFILE，回退 HOME）。
func DefaultTokenPath() (string, error) {
	home := os.Getenv("USERPROFILE")
	if home == "" {
		home = os.Getenv("HOME")
	}
	if home == "" {
		return "", errors.New("无法确定用户主目录（USERPROFILE/HOME 均为空）")
	}
	return filepath.Join(home, filepath.FromSlash(defaultTokenRelPath)), nil
}

// TokenFile 是凭据文件的落盘结构（只读）。
// scopes 在不同版本里可能是字符串或字符串数组，本工具不使用该字段，
// 按宽松类型解码以兼容两种形态。
type TokenFile struct {
	ClientID     string          `json:"client_id"`
	ClientSecret string          `json:"client_secret"`
	RefreshToken string          `json:"refresh_token"`
	TokenURI     string          `json:"token_uri"`
	Scopes       json.RawMessage `json:"scopes"`
	ProjectID    string          `json:"project_id"`
}

// LoadTokenFile 读取并校验凭据文件；文件不存在返回包内 os.ErrNotExist 包装错误。
func LoadTokenFile(path string) (*TokenFile, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取凭据文件 %s: %w", path, err)
	}
	var tf TokenFile
	if err := json.Unmarshal(b, &tf); err != nil {
		return nil, fmt.Errorf("解析凭据文件 %s: %w", path, err)
	}
	if tf.ClientID == "" || tf.ClientSecret == "" || tf.RefreshToken == "" {
		return nil, fmt.Errorf("凭据文件 %s 缺少 client_id / client_secret / refresh_token", path)
	}
	if tf.TokenURI == "" {
		tf.TokenURI = "https://oauth2.googleapis.com/token"
	}
	return &tf, nil
}

var oauthHTTPClient = &http.Client{Timeout: 30 * time.Second}

// RefreshAccessToken 用 refresh_token 换取新的 access token。
// 不持久化返回的 token（进程零写入）；Google 对已用 refresh_token 保持有效。
func RefreshAccessToken(ctx context.Context, tf *TokenFile) (string, error) {
	form := url.Values{
		"client_id":     {tf.ClientID},
		"client_secret": {tf.ClientSecret},
		"refresh_token": {tf.RefreshToken},
		"grant_type":    {"refresh_token"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tf.TokenURI, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("构造 OAuth 请求: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := oauthHTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("请求 OAuth 端点: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("OAuth 端点返回 %d: %s", resp.StatusCode, snippet(body))
	}
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &tok); err != nil {
		return "", fmt.Errorf("解析 OAuth 响应: %w", err)
	}
	if tok.AccessToken == "" {
		return "", errors.New("OAuth 响应缺少 access_token")
	}
	return tok.AccessToken, nil
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}
