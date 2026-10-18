package engine

import (
	"strings"
	"testing"
)

func TestGetFilePublisher(t *testing.T) {
	pub := GetFilePublisher(`C:\Windows\System32\cmd.exe`)
	t.Logf("cmd.exe publisher: %s", pub)
	if !strings.Contains(pub, "Microsoft") {
		t.Errorf("Expected cmd.exe to contain Microsoft, got: %s", pub)
	}
}