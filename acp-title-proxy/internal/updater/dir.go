package updater

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
)

// SanitizePathComponent 将版本字符串转化为 Zed 认可的安全路径组件。
// 与 Zed 源码 crates/project/src/agent_server_store.rs 的 sanitize_path_component 完全对齐：
// 保留 'a'..='z' | 'A'..='Z' | '0'..='9' | '.' | '_' | '-'，其他字符替换为 '-'。
func SanitizePathComponent(component string) string {
	var sb strings.Builder
	for _, r := range component {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-':
			sb.WriteRune(r)
		default:
			sb.WriteRune('-')
		}
	}
	res := sb.String()
	if res == "" {
		return "unknown"
	}
	return res
}

// VersionedArchiveCacheDir 计算 Zed 官方命名规范的缓存版本目录路径。
// 与 Zed 源码 crates/project/src/agent_server_store.rs 的 versioned_archive_cache_dir 完全对齐：
// dir = v_{sanitized_version}_{sha256(version)[:16]}_{sha256(archive_url + "\0sha256:" + sha256.lower())[:16]}
func VersionedArchiveCacheDir(baseDir, version, archiveURL, sha256Hex string) string {
	sanitized := SanitizePathComponent(version)

	vHasher := sha256.New()
	vHasher.Write([]byte(version))
	vHash := hex.EncodeToString(vHasher.Sum(nil))[:16]

	aHasher := sha256.New()
	aHasher.Write([]byte(archiveURL))
	if sha256Hex != "" {
		aHasher.Write([]byte("\x00sha256:"))
		aHasher.Write([]byte(strings.ToLower(sha256Hex)))
	}
	aHash := hex.EncodeToString(aHasher.Sum(nil))[:16]

	dirName := fmt.Sprintf("v_%s_%s_%s", sanitized, vHash, aHash)
	return filepath.Join(baseDir, dirName)
}
