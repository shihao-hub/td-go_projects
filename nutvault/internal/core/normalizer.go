package core

import (
	"path/filepath"
	"strings"
	"time"
)

// CleanPath 将输入路径转换为规范的 Windows 绝对路径
// 若文件或目录在本地已存在，将还原其实际磁盘目录大小写（如 c:\users -> C:\Users）
func CleanPath(pathStr string) string {
	cleaned := filepath.Clean(strings.TrimSpace(pathStr))
	if abs, err := filepath.Abs(cleaned); err == nil {
		cleaned = abs
	}
	if realPath, err := filepath.EvalSymlinks(cleaned); err == nil {
		cleaned = realPath
	}
	// 确保如果是盘符统一为大写规范 C:\
	if len(cleaned) >= 2 && cleaned[1] == ':' {
		cleaned = strings.ToUpper(cleaned[:1]) + cleaned[1:]
	}
	return cleaned
}

// NormalizeDir 将任意绝对目录转换为安全的扁平目录名
// 例如: C:\Users\29580\AppData\Roaming\Zed -> C-Users-29580-AppData-Roaming-Zed
func NormalizeDir(dirPath string) string {
	cleaned := CleanPath(dirPath)

	parts := strings.FieldsFunc(cleaned, func(r rune) bool {
		return r == '\\' || r == '/' || r == ':' || r == '*' || r == '?' || r == '"' || r == '<' || r == '>' || r == '|'
	})

	var nonEmpties []string
	for _, p := range parts {
		trimmed := strings.Trim(p, " -_.")
		if trimmed != "" {
			nonEmpties = append(nonEmpties, trimmed)
		}
	}

	normalized := strings.Join(nonEmpties, "-")
	if normalized == "" {
		normalized = "root"
	}
	return normalized
}

// FormatSnapshotFilename 为文件名注入时间戳，格式例如 settings-20261026225218.json
func FormatSnapshotFilename(originalFilename string, t time.Time) string {
	ext := filepath.Ext(originalFilename)
	base := strings.TrimSuffix(originalFilename, ext)
	timestamp := t.Format("20060102150405")
	if ext != "" {
		return base + "-" + timestamp + ext
	}
	return base + "-" + timestamp
}

// TargetInfo 解析一个文件路径的拆分信息
type TargetInfo struct {
	SourcePath string // 规范绝对路径: C:\Users\29580\AppData\Roaming\Zed\settings.json
	SourceDir  string // 父目录: C:\Users\29580\AppData\Roaming\Zed
	FileName   string // 文件名: settings.json
	MappedDir  string // 映射的坚果云目录: C-Users-29580-AppData-Roaming-Zed
}

// AnalyzePath 解析文件路径并计算映射规则
func AnalyzePath(filePath string) TargetInfo {
	clean := CleanPath(filePath)
	dir := filepath.Dir(clean)
	file := filepath.Base(clean)
	mapped := NormalizeDir(dir)
	return TargetInfo{
		SourcePath: clean,
		SourceDir:  dir,
		FileName:   file,
		MappedDir:  mapped,
	}
}
