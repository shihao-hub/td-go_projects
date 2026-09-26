package clipboard

import (
	"context"
	"os/exec"
	"strings"
	"syscall"
)

const createNoWindow = 0x08000000

func captureImage(ctx context.Context, destPath string, run CommandRunner) (string, error) {
	const script = `ErrorActionPreference = 'Stop'; ` +
		`Add-Type -AssemblyName System.Windows.Forms; ` +
		`Add-Type -AssemblyName System.Drawing; ` +
		`$path = '{{PATH}}'; ` +
		`$image = [System.Windows.Forms.Clipboard]::GetImage(); ` +
		`if ($null -eq $image) { Write-Output 'empty' } else { $image.Save($path, [System.Drawing.Imaging.ImageFormat]::Png); Write-Output 'ok' }`
	escapedPath := strings.ReplaceAll(destPath, "'", "''")
	result, err := run(ctx, "powershell", []string{
		"-NoProfile", "-NonInteractive", "-STA", "-Command",
		strings.ReplaceAll(script, "{{PATH}}", escapedPath),
	})
	if err != nil {
		return "", ErrUnavailable
	}
	return strings.TrimSpace(result), nil
}

func systemRun(ctx context.Context, command string, args []string) (string, error) {
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow,
	}
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(output), nil
}
