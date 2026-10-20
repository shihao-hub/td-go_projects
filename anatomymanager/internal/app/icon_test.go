package app

import (
	"testing"

	"github.com/wailsapp/wails/v3/pkg/w32"
)

func TestIconLoading(t *testing.T) {
	if len(iconICO) == 0 {
		t.Fatalf("iconICO is empty")
	}
	hIcon, err := w32.CreateLargeHIconFromImage(iconICO)
	if err != nil {
		t.Fatalf("CreateLargeHIconFromImage failed: %v", err)
	}
	if hIcon == 0 {
		t.Fatalf("CreateLargeHIconFromImage returned 0 handle")
	}
	t.Logf("CreateLargeHIconFromImage succeeded, hIcon = %v", hIcon)

	hSmall, err := w32.CreateSmallHIconFromImage(iconICO)
	if err != nil {
		t.Fatalf("CreateSmallHIconFromImage failed: %v", err)
	}
	if hSmall == 0 {
		t.Fatalf("CreateSmallHIconFromImage returned 0 handle")
	}
	t.Logf("CreateSmallHIconFromImage succeeded, hSmall = %v", hSmall)
}
