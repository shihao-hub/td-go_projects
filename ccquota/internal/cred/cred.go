// Package cred 读取各 CLI 已有的登录凭据。
//
// 只读取，不刷新、不回写：token 过期时交由调用方提示用户重新登录。
package cred

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CodexToken 是从 ~/.codex/auth.json 取得的访问凭据。
type CodexToken struct {
	AccessToken string
	AccountID   string
}

// LoadCodexToken 读取 Codex 的 ChatGPT 登录态。
func LoadCodexToken() (CodexToken, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return CodexToken{}, fmt.Errorf("无法定位用户目录: %w", err)
	}
	path := filepath.Join(home, ".codex", "auth.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return CodexToken{}, fmt.Errorf("未找到 Codex 登录态（%s）", path)
	}
	var payload struct {
		AuthMode string `json:"auth_mode"`
		Tokens   struct {
			AccessToken string `json:"access_token"`
			AccountID   string `json:"account_id"`
		} `json:"tokens"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return CodexToken{}, fmt.Errorf("解析 Codex 登录态失败: %w", err)
	}
	if strings.TrimSpace(payload.Tokens.AccessToken) == "" {
		return CodexToken{}, fmt.Errorf("Codex 登录态缺少 access_token，请重新登录")
	}
	return CodexToken{
		AccessToken: payload.Tokens.AccessToken,
		AccountID:   payload.Tokens.AccountID,
	}, nil
}

// LoadKimiToken 读取 Kimi Code 的 OAuth 访问令牌。
//
// 凭据真身在 ~/.kimi-code/credentials/*.json（access_token + expires_at），
// 由 Kimi Code CLI 与 cc-switch 共用；~/.cc-switch/kimi_accounts.json 只存
// 账号展示信息，拿其中的 token 去请求额度会得到 401。
func LoadKimiToken(providerID string) (string, error) {
	_ = providerID
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("无法定位用户目录: %w", err)
	}
	dir := filepath.Join(home, ".kimi-code", "credentials")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("未找到 Kimi Code 凭据目录（%s），请先登录 Kimi Code CLI", dir)
	}

	var (
		bestToken   string
		bestExpiry  int64
		expiredSeen bool
	)
	now := time.Now().Unix()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var payload struct {
			AccessToken string `json:"access_token"`
			ExpiresAt   int64  `json:"expires_at"`
		}
		if err := json.Unmarshal(raw, &payload); err != nil {
			continue
		}
		if strings.TrimSpace(payload.AccessToken) == "" {
			continue
		}
		if payload.ExpiresAt > 0 && payload.ExpiresAt < now {
			expiredSeen = true
			continue
		}
		if payload.ExpiresAt >= bestExpiry {
			bestExpiry = payload.ExpiresAt
			bestToken = payload.AccessToken
		}
	}
	if bestToken != "" {
		return bestToken, nil
	}
	if expiredSeen {
		return "", fmt.Errorf("Kimi 登录已过期：打开一次 Kimi Code CLI 即可自动刷新令牌")
	}
	return "", fmt.Errorf("Kimi 凭据为空，请登录 Kimi Code CLI")
}

// Mask 返回用于日志的掩码：仅保留前 4 位与后 4 位。
func Mask(secret string) string {
	if len(secret) <= 8 {
		if secret == "" {
			return ""
		}
		return "****"
	}
	return secret[:4] + "…" + secret[len(secret)-4:]
}
