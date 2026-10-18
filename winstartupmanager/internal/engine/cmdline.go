package engine

import (
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

// ExpandEnvStrings expands %VAR% in Windows paths using Win32 API.
func ExpandEnvStrings(s string) string {
	if !strings.Contains(s, "%") {
		return s
	}
	src, err := syscall.UTF16PtrFromString(s)
	if err != nil {
		return s
	}
	size, err := windows.ExpandEnvironmentStrings(src, nil, 0)
	if size == 0 {
		return s
	}
	buf := make([]uint16, size)
	_, err = windows.ExpandEnvironmentStrings(src, &buf[0], size)
	if err != nil {
		return s
	}
	return syscall.UTF16ToString(buf)
}

// ParseCommandLine splits a raw startup command line into ExePath and Arguments.
func ParseCommandLine(raw string) (exePath string, args string) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", ""
	}

	// 1. Quoted path: "C:\Program Files\App\app.exe" --arg1 /arg2
	if strings.HasPrefix(trimmed, "\"") {
		closingQuote := strings.Index(trimmed[1:], "\"")
		if closingQuote != -1 {
			exe := trimmed[1 : closingQuote+1]
			rem := strings.TrimSpace(trimmed[closingQuote+2:])
			return cleanExePath(exe), rem
		}
		return cleanExePath(strings.Trim(trimmed, "\"")), ""
	}

	// 2. Contains .exe, .bat, .cmd (case-insensitive)
	lower := strings.ToLower(trimmed)
	for _, ext := range []string{".exe", ".cmd", ".bat", ".scr"} {
		idx := strings.Index(lower, ext)
		if idx != -1 {
			end := idx + len(ext)
			if end == len(trimmed) || trimmed[end] == ' ' || trimmed[end] == '\t' {
				exe := trimmed[:end]
				rem := strings.TrimSpace(trimmed[end:])
				return cleanExePath(exe), rem
			}
		}
	}

	// 3. Simple space-delimited split
	parts := strings.SplitN(trimmed, " ", 2)
	exe := parts[0]
	rem := ""
	if len(parts) > 1 {
		rem = strings.TrimSpace(parts[1])
	}
	return cleanExePath(exe), rem
}

func cleanExePath(p string) string {
	unquoted := strings.Trim(p, "\"")
	expanded := ExpandEnvStrings(unquoted)
	return filepath.Clean(expanded)
}