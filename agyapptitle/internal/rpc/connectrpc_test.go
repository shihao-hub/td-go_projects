package rpc

import (
	"testing"
)

func TestDetectRunningLanguageServer(t *testing.T) {
	info, err := DetectRunningLanguageServer()
	if err != nil {
		t.Logf("Language server not running or detection failed: %v", err)
		return
	}
	t.Logf("Detected language server: PID=%d, Port=%d, HTTPS=%v, CSRFTokenLen=%d",
		info.PID, info.Port, info.IsHTTPS, len(info.CSRFToken))

	if info.Port == 0 || info.CSRFToken == "" {
		t.Errorf("invalid detected info: %+v", info)
	}
}
