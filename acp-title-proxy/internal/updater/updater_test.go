package updater

import (
	"testing"
)

func TestCheck(t *testing.T) {
	res, err := Check(true)
	if err != nil {
		t.Fatalf("Check(true) failed: %v", err)
	}

	t.Logf("Current installed: %s", res.CurrentVersion)
	t.Logf("Latest version:   %s", res.LatestVersion)
	t.Logf("HasUpdate:        %v", res.HasUpdate)
	t.Logf("AlreadyInstalled: %v", res.AlreadyInstalled)
	t.Logf("TargetDir:        %s", res.TargetDir)

	if res.LatestVersion == "" {
		t.Fatalf("Expected non-empty LatestVersion")
	}
}
