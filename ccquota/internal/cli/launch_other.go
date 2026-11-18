//go:build !windows

package cli

// launchedFromExplorer 在非 Windows 平台没有「双击新建控制台」的语义。
func launchedFromExplorer() bool {
	return false
}
