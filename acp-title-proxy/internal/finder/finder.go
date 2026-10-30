package finder

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// FindOpenCode 定位 opencode 可执行文件。
func FindOpenCode(customPath string) (string, []string, error) {
	if customPath != "" {
		if _, err := os.Stat(customPath); err == nil {
			return customPath, []string{"acp"}, nil
		}
	}

	// 1. 查询 Zed 注册表
	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData != "" {
		pattern := filepath.Join(localAppData, "Zed", "external_agents", "registry", "opencode", "*", "opencode.exe")
		matches, err := filepath.Glob(pattern)
		if err == nil && len(matches) > 0 {
			// 取版本最新的匹配项
			return matches[len(matches)-1], []string{"acp"}, nil
		}
	}

	// 2. 查询 PATH
	if path, err := exec.LookPath("opencode"); err == nil {
		return path, []string{"acp"}, nil
	}

	return "", nil, fmt.Errorf("could not find opencode.exe in Zed registry or system PATH")
}

// FindAGY 定位 agy_acp_server 可执行文件。
func FindAGY(customPath string) (string, []string, error) {
	if customPath != "" {
		if _, err := os.Stat(customPath); err == nil {
			return customPath, nil, nil
		}
	}

	// 1. 查询 Zed 注册表
	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData != "" {
		pattern := filepath.Join(localAppData, "Zed", "external_agents", "registry", "antigravity-acp", "*", "agy_acp_server.exe")
		matches, err := filepath.Glob(pattern)
		if err == nil && len(matches) > 0 {
			// 取版本最新的匹配项
			return matches[len(matches)-1], nil, nil
		}
	}

	// 2. 查询 PATH
	if path, err := exec.LookPath("agy_acp_server"); err == nil {
		return path, nil, nil
	}

	return "", nil, fmt.Errorf("could not find agy_acp_server.exe in Zed registry or system PATH")
}
