package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	csrfTokenRegex = regexp.MustCompile(`--csrf_token\s+([a-zA-Z0-9\-]+)`)
)

// LanguageServerInfo 保存探测到的正在运行的语言服务器进程与鉴权信息。
type LanguageServerInfo struct {
	PID       int
	CSRFToken string
	Port      int
}

// DetectRunningLanguageServer 探测系统中运行的 language_server.exe。
func DetectRunningLanguageServer() (*LanguageServerInfo, error) {
	// 使用 powershell 一次性查询 PID、命令行与监听端口
	psScript := `
$p = Get-CimInstance Win32_Process -Filter "Name='language_server.exe'" | Select-Object -First 1 ProcessId, CommandLine
if (-not $p) { exit 1 }
$token = if ($p.CommandLine -match '--csrf_token\s+([a-zA-Z0-9\-]+)') { $matches[1] } else { "" }
$ports = Get-NetTCPConnection -OwningProcess $p.ProcessId -State Listen | Select-Object -ExpandProperty LocalPort
[PSCustomObject]@{ PID = $p.ProcessId; Token = $token; Ports = $ports } | ConvertTo-Json -Compress
`

	cmd := exec.Command("powershell", "-NoProfile", "-Command", psScript)
	out, err := cmd.Output()
	if err != nil || len(strings.TrimSpace(string(out))) == 0 {
		return nil, fmt.Errorf("language_server.exe is not running")
	}

	var raw struct {
		PID   int         `json:"PID"`
		Token string      `json:"Token"`
		Ports interface{} `json:"Ports"`
	}

	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, err
	}

	if raw.Token == "" {
		return nil, fmt.Errorf("csrf_token not found in command line")
	}

	var ports []int
	switch v := raw.Ports.(type) {
	case float64:
		ports = append(ports, int(v))
	case []interface{}:
		for _, item := range v {
			if f, ok := item.(float64); ok {
				ports = append(ports, int(f))
			} else if s, ok := item.(string); ok {
				if n, e := strconv.Atoi(s); e == nil {
					ports = append(ports, n)
				}
			}
		}
	}

	// 探测哪个端口提供 HTTP 200 服务
	httpClient := &http.Client{Timeout: 500 * time.Millisecond}
	var matchedPort int

	for _, p := range ports {
		url := fmt.Sprintf("http://127.0.0.1:%d/", p)
		req, _ := http.NewRequest("GET", url, nil)
		req.Header.Set("User-Agent", "antigravity")
		resp, err := httpClient.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == 200 {
				matchedPort = p
				break
			}
		}
	}

	if matchedPort == 0 && len(ports) > 0 {
		// 回退第一个端口
		matchedPort = ports[0]
	}

	if matchedPort == 0 {
		return nil, fmt.Errorf("no active HTTP port found for language_server.exe")
	}

	return &LanguageServerInfo{
		PID:       raw.PID,
		CSRFToken: raw.Token,
		Port:      matchedPort,
	}, nil
}

// PushConversationTitle 通过 ConnectRPC 实时热推送单条会话标题至 language_server.exe 内存。
func PushConversationTitle(info *LanguageServerInfo, sessionID, newTitle string) error {
	if info == nil || info.Port == 0 || info.CSRFToken == "" {
		return fmt.Errorf("invalid language server connection info")
	}

	rpcURL := fmt.Sprintf("http://127.0.0.1:%d/exa.language_server_pb.LanguageServerService/UpdateConversationAnnotations", info.Port)

	payload := map[string]interface{}{
		"cascadeIds": []string{sessionID},
		"annotations": map[string]interface{}{
			"title": newTitle,
		},
		"mergeAnnotations": true,
	}

	jsonBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", rpcURL, bytes.NewReader(jsonBytes))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-codeium-csrf-token", info.CSRFToken)
	req.Header.Set("User-Agent", "antigravity")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("connectrpc returned status %d", resp.StatusCode)
	}

	return nil
}
