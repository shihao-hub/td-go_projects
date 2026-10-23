package core

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"taskbarguard"
	"taskbarguard/internal/buildinfo"
)

// embeddedHashName 是脚本释放目录内的内容指纹文件，用于判断是否需要重新释放。
const embeddedHashName = ".embedded-scripts.sha256"

// MaterializeEmbeddedScripts 把内嵌的 scripts 资产释放到 dataDir/scripts。
//
// 供单 exe 分发使用：外置 scripts 目录不存在时调用。指纹一致直接复用，
// 否则释放到临时目录再整体换入；旧目录被运行中的实例占用而无法替换时，
// 退回旧目录继续工作。
func MaterializeEmbeddedScripts(dataDir string) (string, error) {
	if dataDir == "" {
		return "", errors.New("无法确定数据目录，不能释放内嵌脚本")
	}
	root, err := fs.Sub(taskbarguard.Scripts, "scripts")
	if err != nil {
		return "", fmt.Errorf("读取内嵌脚本失败: %w", err)
	}
	digest := buildinfo.ScriptsHash
	if digest == "" {
		if digest, err = hashFS(root); err != nil {
			return "", err
		}
	}

	target := filepath.Join(dataDir, "scripts")
	if current, readErr := os.ReadFile(filepath.Join(target, embeddedHashName)); readErr == nil && string(current) == digest {
		return target, nil
	}

	temporary := target + ".tmp"
	if err := os.RemoveAll(temporary); err != nil {
		return "", fmt.Errorf("清理临时脚本目录失败: %w", err)
	}
	if err := extractFS(root, temporary); err != nil {
		_ = os.RemoveAll(temporary)
		return "", err
	}
	if err := os.WriteFile(filepath.Join(temporary, embeddedHashName), []byte(digest), 0o644); err != nil {
		_ = os.RemoveAll(temporary)
		return "", fmt.Errorf("写入脚本指纹失败: %w", err)
	}

	if err := os.RemoveAll(target); err != nil {
		_ = os.RemoveAll(temporary)
		if info, statErr := os.Stat(target); statErr == nil && info.IsDir() {
			return target, nil
		}
		return "", fmt.Errorf("替换脚本目录失败: %w", err)
	}
	if err := os.Rename(temporary, target); err != nil {
		return "", fmt.Errorf("启用新脚本目录失败: %w", err)
	}
	return target, nil
}

// hashFS 计算文件树内容指纹，路径与内容都参与，保证款式资产变更也能被识别。
func hashFS(root fs.FS) (string, error) {
	digest := sha256.New()
	err := fs.WalkDir(root, ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		data, err := fs.ReadFile(root, path)
		if err != nil {
			return err
		}
		digest.Write([]byte(filepath.ToSlash(path)))
		digest.Write([]byte{0})
		digest.Write(data)
		digest.Write([]byte{0})
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("计算内嵌脚本指纹失败: %w", err)
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

// extractFS 把内嵌文件树写入本地目录，保留相对路径与嵌套结构。
func extractFS(root fs.FS, destination string) error {
	return fs.WalkDir(root, ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		target := filepath.Join(destination, filepath.FromSlash(path))
		if entry.IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("创建脚本目录失败: %w", err)
			}
			return nil
		}
		data, err := fs.ReadFile(root, path)
		if err != nil {
			return err
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			return fmt.Errorf("释放脚本文件失败 %s: %w", path, err)
		}
		return nil
	})
}
