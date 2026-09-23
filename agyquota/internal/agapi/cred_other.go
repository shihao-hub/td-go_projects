//go:build !windows

package agapi

// GetAgyLoggedEmail 非 Windows 平台回退
func GetAgyLoggedEmail() string {
	return ""
}
