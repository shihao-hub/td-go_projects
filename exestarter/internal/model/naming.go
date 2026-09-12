package model

import (
	"path/filepath"
	"strings"
)

// ExeBaseName 去掉目录和 .exe 后缀，作为条目默认名称。
func ExeBaseName(path string) string {
	name := filepath.Base(path)
	return strings.TrimSuffix(name, filepath.Ext(name))
}
