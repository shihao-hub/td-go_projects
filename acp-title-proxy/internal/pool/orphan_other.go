//go:build !windows

package pool

// ReapOrphans 在非 Windows 平台为空实现：AGY 桌面端仅在 Windows 上存在该泄漏形态。
func ReapOrphans(dryRun bool) ReapResult {
	return ReapResult{DryRun: dryRun}
}
