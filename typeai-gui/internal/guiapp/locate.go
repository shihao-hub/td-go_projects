// locate.go：typeai.exe 定位（环境变量 → 同目录 → PATH）。
package guiapp

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// EnvTypeaiPath 覆盖 typeai.exe 位置的环境变量名。
const EnvTypeaiPath = "TYPEAI_GUI_TYPEAI_PATH"

// LocateTypeai 按优先级定位 typeai.exe：
//  1. 环境变量 TYPEAI_GUI_TYPEAI_PATH（显式指定）
//  2. 与 typeai-gui.exe 同目录的 typeai.exe（便携部署）
//  3. PATH 中的 typeai.exe
//
// 找不到时返回包含全部已尝试位置的错误，供界面直接展示。
func LocateTypeai() (string, error) {
	var tried []string

	if p := os.Getenv(EnvTypeaiPath); p != "" {
		tried = append(tried, fmt.Sprintf("%s=%s", EnvTypeaiPath, p))
		if abs, err := filepath.Abs(p); err == nil {
			if info, statErr := os.Stat(abs); statErr == nil && !info.IsDir() {
				return abs, nil
			}
		}
	}

	if exe, err := os.Executable(); err == nil {
		cand := filepath.Join(filepath.Dir(exe), "typeai.exe")
		tried = append(tried, cand)
		if info, statErr := os.Stat(cand); statErr == nil && !info.IsDir() {
			return cand, nil
		}
	}

	if p, err := exec.LookPath("typeai.exe"); err == nil {
		if abs, aerr := filepath.Abs(p); aerr == nil {
			return abs, nil
		}
		return p, nil
	}
	tried = append(tried, "PATH")

	return "", fmt.Errorf(
		"未找到 typeai.exe，已尝试位置：%s。可将 typeai.exe 放在本程序同目录，或用环境变量 %s 显式指定",
		strings.Join(tried, "、"), EnvTypeaiPath)
}
