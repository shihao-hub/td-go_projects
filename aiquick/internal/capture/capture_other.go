//go:build !windows

package capture

// SelectedText 非 Windows 平台不支持划词。
func SelectedText(maxLen int) (string, bool) { return "", false }
