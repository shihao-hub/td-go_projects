package service

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseImageMarkers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "image with space.png")
	writeTestPNG(t, path)
	input := "look\n[[image:" + path + "]]\nat this\n[[image:" + path + "]]\nplease"

	text, images, err := ParseImageMarkers(input)
	if err != nil {
		t.Fatalf("ParseImageMarkers() error = %v", err)
	}
	if text != "look\nat this\nplease" {
		t.Errorf("text = %q", text)
	}
	if len(images) != 2 || images[0].Path != filepath.Clean(path) || images[1].Base64 == "" {
		t.Fatalf("images = %#v", images)
	}
}

func TestParseImageMarkerInvalidPath(t *testing.T) {
	_, _, err := ParseImageMarkers("look [[image:" + filepath.Join(t.TempDir(), "missing.png") + "]] now")
	var operation *OperationError
	if !errors.As(err, &operation) || operation.Code != "invalid_image" {
		t.Fatalf("err = %#v, want invalid_image OperationError", err)
	}
}

func TestParseImageMarkersWithoutMarkers(t *testing.T) {
	input := "  keep\n\n  original text  "
	if got, _, err := ParseImageMarkers(input); err != nil || got != strings.TrimSpace(input) {
		t.Fatalf("ParseImageMarkers() = %q, %v", got, err)
	}
}
