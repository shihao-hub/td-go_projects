package env

// Mode 表示当前编译目标环境。
type Mode string

const (
	ModeDev  Mode = "dev"
	ModeProd Mode = "prod"
)

// SingleInstanceID 返回用于单例互斥的唯一 ID。
func SingleInstanceID() string {
	if IsDev() {
		return "shihao.langproj.glmquotawatch-gui.dev"
	}
	return "shihao.langproj.glmquotawatch-gui"
}

// WindowTitle 返回 GUI 主窗口标题。
func WindowTitle() string {
	if IsDev() {
		return "GLM 用量监控 [DEV]"
	}
	return "GLM 用量监控"
}

// DataSubDir 返回隔离的数据子目录名称。
func DataSubDir() string {
	if IsDev() {
		return "dev"
	}
	return "prod"
}

// AutostartAllowed 返回当前环境是否允许注册开机自启。
func AutostartAllowed() bool {
	return !IsDev()
}