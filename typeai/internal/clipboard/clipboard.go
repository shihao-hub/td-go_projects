// Package clipboard captures images from the platform clipboard on demand.
package clipboard

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

var captureTimeout = 5 * time.Second

var (
	ErrEmpty       = errors.New("clipboard has no image")
	ErrUnavailable = errors.New("clipboard image capture is unavailable")
)

// CommandRunner executes one platform clipboard helper command.
type CommandRunner func(ctx context.Context, command string, args []string) (string, error)

// ClipboardImageDir returns the project-owned temporary capture directory.
func ClipboardImageDir() string {
	return filepath.Join(os.TempDir(), "language_projects", "typeai", "clipboard")
}

// CaptureImage reads an image from the current clipboard and saves it as PNG.
func CaptureImage() (string, error) {
	return CaptureImageWithRunner(ClipboardImageDir(), systemRun)
}

// CaptureImageWithRunner is the testable capture entry point.
func CaptureImageWithRunner(destDir string, run CommandRunner) (string, error) {
	if run == nil {
		return "", ErrUnavailable
	}
	if err := os.MkdirAll(destDir, 0o700); err != nil {
		return "", fmt.Errorf("create clipboard directory: %w", err)
	}

	destPath, err := destinationPath(destDir)
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), captureTimeout)
	defer cancel()
	output, err := captureImage(ctx, destPath, run)
	if err != nil {
		return "", err
	}
	switch output {
	case "ok":
		return destPath, nil
	case "empty":
		return "", ErrEmpty
	default:
		return "", ErrUnavailable
	}
}

func destinationPath(destDir string) (string, error) {
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", fmt.Errorf("generate clipboard file name: %w", err)
	}
	name := time.Now().Format("20060102-150405") + "-" + hex.EncodeToString(random[:]) + ".png"
	return filepath.Join(destDir, name), nil
}
