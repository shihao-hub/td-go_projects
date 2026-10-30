package finder

import (
	"testing"
)

func TestFindBinaries(t *testing.T) {
	ocPath, ocArgs, err := FindOpenCode("")
	if err != nil {
		t.Fatalf("FindOpenCode failed: %v", err)
	}
	t.Logf("Found OpenCode at: %s with args: %v", ocPath, ocArgs)

	agyPath, agyArgs, err := FindAGY("")
	if err != nil {
		t.Fatalf("FindAGY failed: %v", err)
	}
	t.Logf("Found AGY at: %s with args: %v", agyPath, agyArgs)
}
