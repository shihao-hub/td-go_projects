package store

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
)

// ExtractUserPrompt 从本地 transcript.jsonl 中提取第一条用户的实际请求文本。
func ExtractUserPrompt(transcriptPath string) string {
	f, err := os.Open(transcriptPath)
	if err != nil {
		return ""
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	buf := make([]byte, 1024*1024)
	scanner.Buffer(buf, 10*1024*1024)

	for scanner.Scan() {
		var row struct {
			Source  string `json:"source"`
			Type    string `json:"type"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &row); err == nil {
			if row.Type == "USER_INPUT" || row.Source == "USER_EXPLICIT" {
				raw := row.Content
				if start := strings.Index(raw, "<USER_REQUEST>"); start != -1 {
					raw = raw[start+len("<USER_REQUEST>"):]
					if end := strings.Index(raw, "</USER_REQUEST>"); end != -1 {
						raw = raw[:end]
					}
				}
				cleaned := strings.TrimSpace(raw)
				if cleaned != "" {
					return cleaned
				}
			}
		}
	}
	return ""
}
