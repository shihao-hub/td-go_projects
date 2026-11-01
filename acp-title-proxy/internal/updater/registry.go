package updater

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

const (
	// OfficialRegistryURL 是 ACP 官方 Registry CDN 地址
	OfficialRegistryURL = "https://cdn.agentclientprotocol.com/registry/v1/latest/registry.json"
	// AGYAgentID 是 Zed 中 Google Antigravity 的官方 registry ID
	AGYAgentID = "antigravity-acp"
)

// RegistryIndex 对应 registry.json 结构
type RegistryIndex struct {
	Agents []RegistryAgentEntry `json:"agents"`
}

// RegistryAgentEntry 对应 agent 条目
type RegistryAgentEntry struct {
	ID           string               `json:"id"`
	Name         string               `json:"name"`
	Version      string               `json:"version"`
	Description  string               `json:"description,omitempty"`
	Distribution RegistryDistribution `json:"distribution"`
}

// RegistryDistribution 对应分发方式
type RegistryDistribution struct {
	Binary map[string]RegistryBinaryTarget `json:"binary"`
}

// RegistryBinaryTarget 对应特定平台的二进制安装配置
type RegistryBinaryTarget struct {
	Archive string   `json:"archive"`
	Cmd     string   `json:"cmd"`
	Args    []string `json:"args,omitempty"`
	Sha256  string   `json:"sha256"`
}

// DefaultZedRegistryPath 获取本地 Zed registry.json 默认路径
func DefaultZedRegistryPath() string {
	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData == "" {
		return ""
	}
	return filepath.Join(localAppData, "Zed", "external_agents", "registry", "registry.json")
}

// DefaultAGYBaseDir 获取本地 Zed antigravity-acp 安装根目录
func DefaultAGYBaseDir() string {
	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData == "" {
		return ""
	}
	return filepath.Join(localAppData, "Zed", "external_agents", "registry", "antigravity-acp")
}

// CurrentPlatformKey 根据操作系统与架构计算 Zed registry key
func CurrentPlatformKey() string {
	var osKey string
	switch runtime.GOOS {
	case "windows":
		osKey = "windows"
	case "darwin":
		osKey = "darwin"
	case "linux":
		osKey = "linux"
	default:
		osKey = runtime.GOOS
	}

	var archKey string
	switch runtime.GOARCH {
	case "amd64":
		archKey = "x86_64"
	case "arm64":
		archKey = "aarch64"
	default:
		archKey = runtime.GOARCH
	}

	return fmt.Sprintf("%s-%s", osKey, archKey)
}

// LoadRegistry 加载并解析 AGY 官方注册表条目。
// 优先读取本地 Zed 维护的 registry.json；如果本地不存在或 forceRemote 为 true，则通过 HTTP 拉取。
func LoadRegistry(preferLocal bool) (*RegistryAgentEntry, *RegistryBinaryTarget, error) {
	var data []byte
	var err error

	localPath := DefaultZedRegistryPath()
	if preferLocal && localPath != "" {
		if fi, statErr := os.Stat(localPath); statErr == nil && fi.Size() > 0 {
			data, err = os.ReadFile(localPath)
		}
	}

	// 本地不存在或读取失败，则直连官方 CDN
	if len(data) == 0 {
		data, err = fetchRemoteRegistry(15 * time.Second)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to fetch registry (local and remote): %w", err)
		}
	}

	var index RegistryIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return nil, nil, fmt.Errorf("unmarshal registry.json: %w", err)
	}

	var agyEntry *RegistryAgentEntry
	for i := range index.Agents {
		if index.Agents[i].ID == AGYAgentID {
			agyEntry = &index.Agents[i]
			break
		}
	}

	if agyEntry == nil {
		return nil, nil, fmt.Errorf("agent '%s' not found in registry", AGYAgentID)
	}

	platform := CurrentPlatformKey()
	target, ok := agyEntry.Distribution.Binary[platform]
	if !ok {
		return agyEntry, nil, fmt.Errorf("agent '%s' version %s has no binary distribution for platform %s", AGYAgentID, agyEntry.Version, platform)
	}

	return agyEntry, &target, nil
}

func fetchRemoteRegistry(timeout time.Duration) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	client := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
		},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, OfficialRegistryURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "acp-title-proxy-updater")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP status %d: %s", resp.StatusCode, resp.Status)
	}

	return io.ReadAll(resp.Body)
}
