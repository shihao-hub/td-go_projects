// Package instance 集中维护 GUI 单实例互斥标识与跨进程探测逻辑。
//
// Wails 建锁名固定为 wails-app-<UniqueID>-sim；LaunchGUI 建锁与外部探测
// 必须共用本包，否则改名后探测会静默失效。
package instance

// guiIDBase 是 GUI 单实例标识的稳定前缀，dev 通道在其后追加 .dev。
const guiIDBase = "shihao.langproj.typeai.gui"

// GUIID 返回指定通道的 GUI 单实例 UniqueID。
func GUIID(release bool) string {
	if release {
		return guiIDBase
	}
	return guiIDBase + ".dev"
}
