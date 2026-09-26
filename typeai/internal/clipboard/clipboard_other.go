//go:build !windows

package clipboard

import "context"

func captureImage(ctx context.Context, destPath string, run CommandRunner) (string, error) {
	return "", ErrUnavailable
}

func systemRun(ctx context.Context, command string, args []string) (string, error) {
	return "", ErrUnavailable
}
