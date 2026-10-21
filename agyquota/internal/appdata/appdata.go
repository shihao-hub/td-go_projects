// Package appdata 是 agyquota 运行时数据目录的单点解析（仓库强约束）：
// 所有自产文件（地址文件、日志、缓存、静默副本、窗口状态）只允许落在
// %APPDATA%\language_projects\agyquota\<dev|prod>\；取不到 APPDATA 时回退
// ~/.language_projects/agyquota/<dev|prod>/。写前由调用方创建完整目录链。
//
// dev / release 双通道严格隔离：两套二进制各自读写自己的子目录，
// 互不覆盖地址文件、日志与 Zed 缓存。
package appdata

import (
	"io"
	"os"
	"path/filepath"

	"agyquota/internal/buildinfo"
)

const (
	// ProjectName 是数据目录中的项目名。
	ProjectName = "agyquota"
	// ChannelDev / ChannelProd 是双通道子目录名。
	ChannelDev  = "dev"
	ChannelProd = "prod"
	// AddressFileName 是 daemon 地址文件名。
	AddressFileName = "daemon.json"
	// LogFileName 是 daemon 日志文件名（轮转后为 .1 后缀）。
	LogFileName = "daemon.log"

	// TokenCacheFileName 是 Zed Access Token 缓存文件名。
	TokenCacheFileName = ".quota_token_cache.json"
	// SilentBinDirName 是 agy 静默副本所在子目录。
	SilentBinDirName = "bin"
	// SilentBinFileName 是 agy 静默副本文件名。
	SilentBinFileName = "agy_silent.exe"
)

// BaseDir 返回项目数据根目录（未按通道细分）；解析失败返回空串。
func BaseDir() string {
	if appdata := os.Getenv("APPDATA"); appdata != "" {
		return filepath.Join(appdata, "language_projects", ProjectName)
	}
	if home := userHomeDir(); home != "" {
		return filepath.Join(home, ".language_projects", ProjectName)
	}
	return ""
}

// Channel 返回当前构建通道名（dev / prod）。
func Channel() string {
	if buildinfo.IsRelease() {
		return ChannelProd
	}
	return ChannelDev
}

// Dir 返回当前通道的运行时数据目录；解析失败返回空串。
// 首次访问时把旧版单层数据目录中的缓存与静默副本复制到新通道目录（只复制，不删除）。
func Dir() string {
	base := BaseDir()
	if base == "" {
		return ""
	}
	dir := filepath.Join(base, Channel())
	migrateLegacy(base, dir)
	return dir
}

// AddressFile 返回 daemon 地址文件路径。
func AddressFile() string { return filepath.Join(Dir(), AddressFileName) }

// LogFile 返回 daemon 日志文件路径。
func LogFile() string { return filepath.Join(Dir(), LogFileName) }

// migrateLegacy 把旧版 `...\agyquota\` 直下的缓存与静默副本搬到通道目录。
// 只迁移可安全复用的自产数据：地址文件与日志指向旧进程状态，刻意不迁移。
func migrateLegacy(base, dir string) {
	if base == "" || dir == "" || base == dir {
		return
	}
	for _, name := range []string{TokenCacheFileName} {
		copyIfMissing(filepath.Join(base, name), filepath.Join(dir, name))
	}
	srcBin := filepath.Join(base, SilentBinDirName, SilentBinFileName)
	dstBin := filepath.Join(dir, SilentBinDirName, SilentBinFileName)
	copyIfMissing(srcBin, dstBin)
}

// copyIfMissing 仅在源存在且目标不存在时复制（含目标目录创建）。
func copyIfMissing(src, dst string) {
	if _, err := os.Stat(dst); err == nil {
		return
	}
	in, err := os.Open(src)
	if err != nil {
		return
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(dst)
		return
	}
	_ = out.Close()
}

// userHomeDir 依次尝试 USERPROFILE / HOME 环境变量获取用户主目录。
func userHomeDir() string {
	home := os.Getenv("USERPROFILE")
	if home == "" {
		home = os.Getenv("HOME")
	}
	return home
}
