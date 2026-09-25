//go:build !windows

package agapi

// ensureSilentAgyExe 非 Windows 平台无 PE 与 DefTerm 机制，直通返回
func ensureSilentAgyExe(srcPath string) (string, error) {
	return srcPath, nil
}
