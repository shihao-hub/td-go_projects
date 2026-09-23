package agapi

import (
	"bytes"
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

const (
	// ZedQuotaEndpoint Google 生产环境 Cloud Code 配额汇总接口
	ZedQuotaEndpoint = "https://cloudcode-pa.googleapis.com/v1internal:retrieveUserQuotaSummary"

	// ZedUserAgent 严格 1:1 复刻 Zed ACP 官方 User-Agent 结构，避免被风控拦截
	ZedUserAgent = "antigravity/acp/0.1.0 (aidev_client; os_type=windows; arch=amd64; host_path=zed/0.180.0; proxy_client=antigravity/sdk)"

	// DefaultTokenRelPath Zed ACP 凭据默认相对路径
	DefaultTokenRelPath = ".gemini/antigravity-acp/acp_token.json"

	// CacheFileName Access Token 内存级本地缓存，默认缓存 50 分钟，杜绝频繁向 Google 刷新
	CacheFileName = ".quota_token_cache.json"
)

// TokenFile 是 Zed ACP 凭据文件的落盘结构
type TokenFile struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	RefreshToken string `json:"refresh_token"`
	TokenURI     string `json:"token_uri"`
	ProjectID    string `json:"project_id"`
}

// TokenCache 本地 Access Token 缓存
type TokenCache struct {
	AccessToken string    `json:"access_token"`
	ExpiresAt   time.Time `json:"expires_at"`
	Email       string    `json:"email,omitempty"`
}

// ZedClient 是查询 Zed 对应 Antigravity 账号配额的客户端
type ZedClient struct {
	TokenPath  string
	HTTPClient *http.Client
	Logf       func(format string, args ...any)
}

// NewZedClient 创建 Zed 客户端
func NewZedClient(tokenPath string) *ZedClient {
	if tokenPath == "" {
		tokenPath = defaultZedTokenPath()
	}
	tr := &http.Transport{
		Proxy:             http.ProxyFromEnvironment,
		DisableKeepAlives: true, // 避免代理层闲置切断连接引发 EOF
	}
	return &ZedClient{
		TokenPath:  tokenPath,
		HTTPClient: &http.Client{Timeout: 30 * time.Second, Transport: tr},
	}
}

func defaultZedTokenPath() string {
	home := os.Getenv("USERPROFILE")
	if home == "" {
		home = os.Getenv("HOME")
	}
	if home == "" {
		return ""
	}
	return filepath.Join(home, filepath.FromSlash(DefaultTokenRelPath))
}

// FetchUsageWithMeta 通过 Zed ACP 凭据获取配额数据、账号邮箱与凭据到期时间
func (c *ZedClient) FetchUsageWithMeta(ctx context.Context) (*UsageResult, error) {
	if c.TokenPath == "" {
		return nil, errors.New("无法确定 Zed 凭据文件路径（~/.gemini/antigravity-acp/acp_token.json）")
	}

	tf, err := loadTokenFile(c.TokenPath)
	if err != nil {
		return nil, fmt.Errorf("读取 Zed 凭据失败: %w", err)
	}

	// 1. 获取 Access Token 及邮箱元信息（带 50 分钟安全缓存）
	accessToken, email, expiresAt, err := c.getOrRefreshToken(ctx, tf)
	if err != nil {
		return nil, fmt.Errorf("获取认证令牌失败: %w", err)
	}

	// 2. 调用 Google 生产配额汇总接口
	project := tf.ProjectID
	if project == "" {
		project = "aicode-consumers"
	}

	reqBody, _ := json.Marshal(map[string]string{"project": project})
	if c.Logf != nil {
		c.Logf("正在请求 Google Cloud Code 配额接口 (project: %s) ...", project)
	}

	// 带一次遇错退避重试
	var respBytes []byte
	for attempt := 1; attempt <= 2; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, ZedQuotaEndpoint, bytes.NewReader(reqBody))
		if err != nil {
			return nil, fmt.Errorf("构造请求失败: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+accessToken)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", ZedUserAgent)

		resp, err := c.HTTPClient.Do(req)
		if err != nil {
			if attempt < 2 {
				time.Sleep(300 * time.Millisecond)
				continue
			}
			return nil, fmt.Errorf("请求配额接口网络失败: %w", err)
		}

		respBytes, err = io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		_ = resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("读取响应数据失败: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("配额接口返回 HTTP %d: %s", resp.StatusCode, string(respBytes))
		}
		break
	}

	return &UsageResult{
		Raw:       respBytes,
		Account:   email,
		ExpiresAt: &expiresAt,
	}, nil
}

// FetchUsage 兼容纯 []byte 接口
func (c *ZedClient) FetchUsage(ctx context.Context) ([]byte, error) {
	res, err := c.FetchUsageWithMeta(ctx)
	if err != nil {
		return nil, err
	}
	return res.Raw, nil
}

func loadTokenFile(path string) (*TokenFile, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var tf TokenFile
	if err := json.Unmarshal(b, &tf); err != nil {
		return nil, err
	}
	if tf.ClientID == "" || tf.ClientSecret == "" || tf.RefreshToken == "" {
		return nil, errors.New("凭据内容不完整（缺少 client_id / client_secret / refresh_token）")
	}
	if tf.TokenURI == "" {
		tf.TokenURI = "https://oauth2.googleapis.com/token"
	}
	return &tf, nil
}

func (c *ZedClient) getOrRefreshToken(ctx context.Context, tf *TokenFile) (string, string, time.Time, error) {
	cachePath := filepath.Join(filepath.Dir(c.TokenPath), CacheFileName)

	// 检查缓存
	if cacheBytes, err := os.ReadFile(cachePath); err == nil {
		var cache TokenCache
		if json.Unmarshal(cacheBytes, &cache) == nil {
			if cache.AccessToken != "" && time.Now().Before(cache.ExpiresAt) {
				if c.Logf != nil {
					c.Logf("命中本地 Token 缓存（有效期至 %s），无需向 Google 重新刷新", cache.ExpiresAt.Format("15:04:05"))
				}
				// 补全 email（若旧缓存未记录）
				if cache.Email == "" {
					cache.Email = c.fetchEmail(ctx, cache.AccessToken)
					if data, e := json.Marshal(cache); e == nil {
						_ = os.WriteFile(cachePath, data, 0600)
					}
				}
				return cache.AccessToken, cache.Email, cache.ExpiresAt, nil
			}
		}
	}

	// 缓存未命中或已过期，向 Google 请求刷新
	if c.Logf != nil {
		c.Logf("本地 Token 缓存未命中或已过期，正在向 Google OAuth 刷新 Access Token ...")
	}

	form := url.Values{
		"client_id":     {tf.ClientID},
		"client_secret": {tf.ClientSecret},
		"refresh_token": {tf.RefreshToken},
		"grant_type":    {"refresh_token"},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tf.TokenURI, strings.NewReader(form.Encode()))
	if err != nil {
		return "", "", time.Time{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", ZedUserAgent)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", "", time.Time{}, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return "", "", time.Time{}, fmt.Errorf("OAuth 端点返回 HTTP %d: %s", resp.StatusCode, string(body))
	}

	var tokResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"` // 通常为 3599 秒
	}
	if err := json.Unmarshal(body, &tokResp); err != nil {
		return "", "", time.Time{}, fmt.Errorf("解析 OAuth 响应失败: %w", err)
	}
	if tokResp.AccessToken == "" {
		return "", "", time.Time{}, errors.New("OAuth 响应中未包含 access_token")
	}

	// 写入本地缓存，默认缓存 50 分钟（3000 秒）
	validDuration := 50 * time.Minute
	if tokResp.ExpiresIn > 600 {
		validDuration = time.Duration(tokResp.ExpiresIn-600) * time.Second
	}
	expiresAt := time.Now().Add(validDuration)

	email := c.fetchEmail(ctx, tokResp.AccessToken)

	cache := TokenCache{
		AccessToken: tokResp.AccessToken,
		ExpiresAt:   expiresAt,
		Email:       email,
	}
	if cacheData, err := json.Marshal(cache); err == nil {
		_ = os.WriteFile(cachePath, cacheData, 0600)
	}

	return tokResp.AccessToken, email, expiresAt, nil
}

func (c *ZedClient) fetchEmail(ctx context.Context, accessToken string) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://www.googleapis.com/oauth2/v2/userinfo", nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("User-Agent", ZedUserAgent)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	b, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	var uinfo struct {
		Email string `json:"email"`
	}
	_ = json.Unmarshal(b, &uinfo)
	return uinfo.Email
}
