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

	// CacheFileName Access Token 本地缓存，预留 2 分钟安全冗余（缓存约 58 分钟），杜绝频繁向 Google 刷新
	// 注意：该文件只允许落在本项目自有数据目录（见 resolveDataDir），禁止写入 Zed 凭据等外部程序目录
	CacheFileName = ".quota_token_cache.json"

	// ProjectName 用于运行时数据目录命名（%APPDATA%\language_projects\<项目名>）
	ProjectName = "agyquota"
)

// TokenFile 是 Zed ACP 凭据文件的落盘结构
type TokenFile struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	RefreshToken string `json:"refresh_token"`
	TokenURI     string `json:"token_uri"`
	ProjectID    string `json:"project_id"`
}

// TokenCache 本地 Access Token 及配额状态缓存
type TokenCache struct {
	// TokenPath 记录缓存归属的凭据文件路径，加载时校验一致才复用，防止多凭据场景串号
	TokenPath     string          `json:"token_path,omitempty"`
	AccessToken   string          `json:"access_token"`
	ExpiresAt     time.Time       `json:"expires_at"`
	Email         string          `json:"email,omitempty"`
	LastQuotaRaw  json.RawMessage `json:"last_quota_raw,omitempty"`
	LastQuotaAt   time.Time       `json:"last_quota_at,omitempty"`
	GeminiResetAt time.Time       `json:"gemini_reset_at,omitempty"`
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
	if home := userHomeDir(); home != "" {
		return filepath.Join(home, filepath.FromSlash(DefaultTokenRelPath))
	}
	return ""
}

// userHomeDir 依次尝试 USERPROFILE / HOME 环境变量获取用户主目录
func userHomeDir() string {
	home := os.Getenv("USERPROFILE")
	if home == "" {
		home = os.Getenv("HOME")
	}
	return home
}

// resolveDataDir 返回本项目运行时自产数据文件的存放目录（仓库强约束）：
// 优先 %APPDATA%\language_projects\agyquota，取不到 APPDATA 时回退 ~/.language_projects/agyquota/
func resolveDataDir() string {
	if appdata := os.Getenv("APPDATA"); appdata != "" {
		return filepath.Join(appdata, "language_projects", ProjectName)
	}
	if home := userHomeDir(); home != "" {
		return filepath.Join(home, ".language_projects", ProjectName)
	}
	return ""
}

// cachePath 返回 Access Token 缓存文件路径：固定在项目自有数据目录内，与凭据文件位置解耦
func (c *ZedClient) cachePath() string {
	dir := resolveDataDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, CacheFileName)
}

// cleanupLegacyCache 删除历史版本误写在 Zed 凭据目录（外部程序目录）内的缓存文件。
// 该文件由本工具自产，归位清理；只按固定文件名匹配，不触碰目录内其他任何文件。
func (c *ZedClient) cleanupLegacyCache() {
	if c.TokenPath == "" {
		return
	}
	legacy := filepath.Join(filepath.Dir(c.TokenPath), CacheFileName)
	if legacy == c.cachePath() {
		return
	}
	if _, err := os.Stat(legacy); err == nil {
		if rmErr := os.Remove(legacy); rmErr == nil && c.Logf != nil {
			c.Logf("已清理历史版本遗留在凭据目录的缓存文件（已归位至项目数据目录）: %s", legacy)
		}
	}
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

	// 1. 获取 Access Token 及邮箱元信息（优先读本地约 58 分钟安全缓存）
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

	cache, cachePath := c.loadCache()

	// 3. 执行请求，并包含 401 自动失效自愈与 1 次网络重试
	status, respBytes, err := c.doQuotaRequest(ctx, accessToken, reqBody)
	if err != nil || status == http.StatusUnauthorized {
		if status == http.StatusUnauthorized {
			if c.Logf != nil {
				c.Logf("本地 Access Token 已失效 (HTTP 401)，正在向 Google 重新刷新并重试 ...")
			}
			_ = os.Remove(cachePath)
			newTok, newEmail, newExp, refErr := c.refreshToken(ctx, tf, cachePath)
			if refErr == nil {
				accessToken = newTok
				email = newEmail
				expiresAt = newExp
				status, respBytes, err = c.doQuotaRequest(ctx, accessToken, reqBody)
			}
		} else {
			time.Sleep(300 * time.Millisecond)
			status, respBytes, err = c.doQuotaRequest(ctx, accessToken, reqBody)
		}
	}

	if err != nil {
		return nil, fmt.Errorf("请求配额接口网络失败: %w", err)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("配额接口返回 HTTP %d: %s", status, string(respBytes))
	}

	// 4. 防 Google 服务端多副本同步延迟与降级抖动
	hasUsage, resetTime, isDegraded := inspectQuotaStatus(respBytes)

	// 如果当前副本返回的是空/降级响应（如 100% 且缺少 description 字段）
	if isDegraded {
		needRetry := false
		if cache != nil && !cache.GeminiResetAt.IsZero() && time.Now().Before(cache.GeminiResetAt) {
			needRetry = true
		} else {
			// 无缓存或首次查询也做 1 次快速重试，防偶发冷副本
			needRetry = true
		}

		if needRetry {
			for i := 0; i < 2; i++ {
				time.Sleep(150 * time.Millisecond)
				s2, b2, err2 := c.doQuotaRequest(ctx, accessToken, reqBody)
				if err2 == nil && s2 == http.StatusOK {
					h2, r2, deg2 := inspectQuotaStatus(b2)
					if h2 || !deg2 {
						respBytes = b2
						hasUsage = h2
						resetTime = r2
						isDegraded = deg2
						break
					}
				}
			}
		}

		// 重试后若仍未拿到同步数据，且本地有尚未到期的真实配额历史，则自动对齐为有效历史，消除 100% 假象
		if isDegraded && cache != nil && len(cache.LastQuotaRaw) > 0 && !cache.GeminiResetAt.IsZero() && time.Now().Before(cache.GeminiResetAt) {
			if c.Logf != nil {
				c.Logf("提示: 检测到 Google 副本用量数据同步延迟，已自动校准为最新真实配额")
			}
			respBytes = cache.LastQuotaRaw
		}
	}

	// 5. 拿到真实有效用量时更新落盘缓存，记录当前配额快照
	if hasUsage {
		if cache == nil {
			cache = &TokenCache{AccessToken: accessToken, ExpiresAt: expiresAt, Email: email}
		}
		cache.LastQuotaRaw = respBytes
		cache.LastQuotaAt = time.Now()
		cache.GeminiResetAt = resetTime
		c.saveCache(cache, cachePath)
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

func (c *ZedClient) doQuotaRequest(ctx context.Context, accessToken string, reqBody []byte) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ZedQuotaEndpoint, bytes.NewReader(reqBody))
	if err != nil {
		return 0, nil, fmt.Errorf("构造请求失败: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", ZedUserAgent)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()

	b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("读取响应数据失败: %w", err)
	}
	return resp.StatusCode, b, nil
}

type quotaInspector struct {
	Groups []struct {
		DisplayName string `json:"displayName"`
		Name        string `json:"name"`
		Buckets     []struct {
			BucketID          string   `json:"bucketId"`
			RemainingFraction *float64 `json:"remainingFraction"`
			Description       string   `json:"description"`
			ResetTime         string   `json:"resetTime"`
		} `json:"buckets"`
	} `json:"groups"`
}

// inspectQuotaStatus 检查返回的配额中 Gemini 桶的状态
func inspectQuotaStatus(raw []byte) (hasGeminiUsage bool, geminiReset time.Time, isGeminiEmpty bool) {
	var qi quotaInspector
	if err := json.Unmarshal(raw, &qi); err != nil {
		return false, time.Time{}, false
	}
	for _, g := range qi.Groups {
		name := g.DisplayName
		if name == "" {
			name = g.Name
		}
		if name == "Gemini Models" {
			for _, b := range g.Buckets {
				if b.RemainingFraction != nil && *b.RemainingFraction < 0.999 && b.Description != "" {
					hasGeminiUsage = true
					if t, err := time.Parse(time.RFC3339, b.ResetTime); err == nil && t.After(geminiReset) {
						geminiReset = t
					}
				}
			}
			// 判断是否属于空配额（所有桶均为 1.0 且缺少 description 描述）
			allFullWithoutDesc := true
			for _, b := range g.Buckets {
				rem := float64(1)
				if b.RemainingFraction != nil {
					rem = *b.RemainingFraction
				}
				if rem < 0.999 || b.Description != "" {
					allFullWithoutDesc = false
					break
				}
			}
			if allFullWithoutDesc && len(g.Buckets) > 0 {
				isGeminiEmpty = true
			}
		}
	}
	return
}

func (c *ZedClient) loadCache() (*TokenCache, string) {
	c.cleanupLegacyCache()
	cachePath := c.cachePath()
	if cachePath == "" {
		return nil, ""
	}
	b, err := os.ReadFile(cachePath)
	if err != nil {
		return nil, cachePath
	}
	var cache TokenCache
	if err := json.Unmarshal(b, &cache); err != nil {
		return nil, cachePath
	}
	// 缓存归属凭据与当前凭据不一致（含旧版无 token_path 字段）时不复用，防止多凭据串号
	if cache.TokenPath != c.TokenPath {
		return nil, cachePath
	}
	return &cache, cachePath
}

func (c *ZedClient) saveCache(cache *TokenCache, cachePath string) {
	if cache == nil || cachePath == "" {
		return
	}
	// 写前自动创建目录链（含 language_projects 一层）；缓存写盘失败不影响主流程
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		return
	}
	cache.TokenPath = c.TokenPath
	if b, err := json.MarshalIndent(cache, "", "  "); err == nil {
		_ = os.WriteFile(cachePath, b, 0600)
	}
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
	c.cleanupLegacyCache()
	cachePath := c.cachePath()

	// 1. 检查本地缓存是否存在及是否有效
	cacheBytes, err := os.ReadFile(cachePath)
	if err == nil {
		var cache TokenCache
		if jsonErr := json.Unmarshal(cacheBytes, &cache); jsonErr == nil && cache.AccessToken != "" && cache.TokenPath == c.TokenPath {
			now := time.Now()
			if now.Before(cache.ExpiresAt) {
				if c.Logf != nil {
					rem := time.Until(cache.ExpiresAt)
					c.Logf("命中本地 Access Token 缓存（有效期至 %s，还剩 %s），无需向 Google 刷新",
						cache.ExpiresAt.Format("15:04:05"), formatDuration(rem))
				}
				// 补全 email（若旧缓存未记录）
				if cache.Email == "" {
					cache.Email = c.fetchEmail(ctx, cache.AccessToken)
					c.saveCache(&cache, cachePath)
				}
				return cache.AccessToken, cache.Email, cache.ExpiresAt, nil
			}
			// 缓存已到期
			if c.Logf != nil {
				c.Logf("本地 Access Token 缓存已到期（已满 Google 1小时生命周期），正在向 Google OAuth 自动静默续期 ...")
			}
		} else {
			// 缓存格式损坏
			if c.Logf != nil {
				c.Logf("本地 Access Token 缓存文件无效或已损坏，正在向 Google OAuth 重新获取 ...")
			}
		}
	} else if os.IsNotExist(err) {
		// 缓存文件不存在（首次查询或被清理）
		if c.Logf != nil {
			c.Logf("本地未发现 Access Token 缓存（首次查询），正在通过 Zed 凭据向 Google OAuth 获取访问令牌 ...")
		}
	} else {
		// 读取缓存出现其他 I/O 错误
		if c.Logf != nil {
			c.Logf("读取本地 Token 缓存出错 (%v)，正在向 Google OAuth 获取访问令牌 ...", err)
		}
	}

	// 2. 向 Google OAuth 请求刷新
	return c.refreshToken(ctx, tf, cachePath)
}

func (c *ZedClient) refreshToken(ctx context.Context, tf *TokenFile, cachePath string) (string, string, time.Time, error) {
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

	// 写入本地缓存：Google access_token 通常为 3600 秒（1小时）。
	// 预留 2 分钟安全缓冲（即缓存约 58 分钟），最大化利用本地缓存生命周期，
	// 避免频繁刷新，同时防止临界点过期。
	validDuration := 55 * time.Minute
	if tokResp.ExpiresIn > 180 {
		validDuration = time.Duration(tokResp.ExpiresIn-120) * time.Second
	}
	expiresAt := time.Now().Add(validDuration)

	email := c.fetchEmail(ctx, tokResp.AccessToken)

	var existingCache TokenCache
	if existingBytes, err := os.ReadFile(cachePath); err == nil {
		_ = json.Unmarshal(existingBytes, &existingCache)
	}
	// 仅当旧缓存归属同一凭据时才延续其配额历史，防止多凭据串号
	if existingCache.TokenPath != c.TokenPath {
		existingCache = TokenCache{}
	}

	cache := TokenCache{
		AccessToken:   tokResp.AccessToken,
		ExpiresAt:     expiresAt,
		Email:         email,
		LastQuotaRaw:  existingCache.LastQuotaRaw,
		LastQuotaAt:   existingCache.LastQuotaAt,
		GeminiResetAt: existingCache.GeminiResetAt,
	}
	c.saveCache(&cache, cachePath)

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

func formatDuration(d time.Duration) string {
	if d <= 0 {
		return "0秒"
	}
	mins := int(d.Minutes())
	secs := int(d.Seconds()) % 60
	if mins >= 60 {
		return fmt.Sprintf("%d小时%d分钟", mins/60, mins%60)
	}
	if mins > 0 {
		return fmt.Sprintf("%d分钟", mins)
	}
	return fmt.Sprintf("%d秒", secs)
}
