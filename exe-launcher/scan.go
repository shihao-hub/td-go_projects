package main

import (
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

// 噪音目录：依赖缓存/版本库/IDE 配置，里面即使有 exe 也不是想管理的产物。
// 注意保留 dist/：PyStand 等打包产物通常在 dist 下。
var noiseDirs = map[string]bool{
	".git":         true,
	".idea":        true,
	".vscode":      true,
	"__pycache__":  true,
	"node_modules": true,
	".venv":        true,
	"venv":         true,
	"env":          true,
}

func isNoiseDir(name string) bool {
	return noiseDirs[strings.ToLower(name)]
}

const scanMaxResults = 1000

// scanDirExe 递归收集 root 下的 *.exe（大小写不敏感），跳过噪音目录，按路径排序。
// 单个子目录读不了就跳过，不让整体扫描失败。
func scanDirExe(root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != root && isNoiseDir(d.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		if strings.EqualFold(filepath.Ext(d.Name()), ".exe") {
			out = append(out, filepath.Clean(path))
			if len(out) >= scanMaxResults {
				return fs.SkipAll
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

// exeBaseName 去掉目录和 .exe 后缀，作为条目默认名称。
func exeBaseName(path string) string {
	name := filepath.Base(path)
	return strings.TrimSuffix(name, filepath.Ext(name))
}
