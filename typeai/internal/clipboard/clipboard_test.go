package clipboard

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

var scriptPathPattern = regexp.MustCompile(`\$path = '([^']+)'`)

func pngRunner(t *testing.T) CommandRunner {
	t.Helper()
	return func(ctx context.Context, command string, args []string) (string, error) {
		if command != "powershell" {
			t.Errorf("command = %q, want powershell", command)
		}
		joined := strings.Join(args, " ")
		if !strings.Contains(joined, "-STA") {
			t.Error("PowerShell command does not use STA")
		}
		match := scriptPathPattern.FindStringSubmatch(args[len(args)-1])
		if match == nil {
			t.Fatalf("script does not contain destination path: %q", args[len(args)-1])
		}
		if err := os.WriteFile(match[1], []byte{0x89, 'P', 'N', 'G'}, 0o600); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		return "ok", nil
	}
}

func TestCaptureImageWithRunner(t *testing.T) {
	destDir := t.TempDir()
	path, err := CaptureImageWithRunner(destDir, pngRunner(t))
	if err != nil {
		t.Fatalf("CaptureImageWithRunner() error = %v", err)
	}
	if filepath.Dir(path) != filepath.Clean(destDir) || filepath.Ext(path) != ".png" {
		t.Errorf("path = %q", path)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
}

func TestCaptureImageEmpty(t *testing.T) {
	_, err := CaptureImageWithRunner(t.TempDir(), func(context.Context, string, []string) (string, error) {
		return "empty", nil
	})
	if !errors.Is(err, ErrEmpty) {
		t.Fatalf("err = %v, want ErrEmpty", err)
	}
}

func TestCaptureImageUnavailable(t *testing.T) {
	_, err := CaptureImageWithRunner(t.TempDir(), func(context.Context, string, []string) (string, error) {
		return "", errors.New("boom")
	})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
}

func TestCaptureImageTimeout(t *testing.T) {
	oldTimeout := captureTimeout
	captureTimeout = 20 * time.Millisecond
	t.Cleanup(func() { captureTimeout = oldTimeout })
	_, err := CaptureImageWithRunner(t.TempDir(), func(ctx context.Context, _ string, _ []string) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
}
