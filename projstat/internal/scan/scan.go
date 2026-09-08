// Package scan 负责总仓根定位、项目发现与 git 元数据并发采集。
// 所有 git 采集均只读且失败即降级为零值，绝不中断整体输出。
package scan

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"projstat/internal/meta"
)

// langDirs 是 4 个语言子仓目录名；.archived/ 不在此列，永不参与发现。
var langDirs = []string{"go_projects", "python_projects", "rust_projects", "typescript_projects"}

// GitInfo 是单个项目的 git 采集结果；零值表示采集失败或非 git 仓库。
type GitInfo struct {
	LastCommitAt time.Time // 零值 = 采集失败/非仓库
	Tag          string    // 最近可达 tag；无 tag = 空串
}

// Entry 是一个项目在内存中的完整条目：目录信息 + 手工标注 + git 采集。
type Entry struct {
	Dir  string // 相对根："python_projects/zedhub"
	Name string // 目录名
	Lang string // go|python|rust|typescript（由 <lang>_projects 推导）
	Meta *Meta  // nil = 未标注
	Git  GitInfo
}

// Meta 是 meta.Meta 的别名，方便调用方直接使用 scan.Entry 内的字段类型。
type Meta = meta.Meta

// FindRoot 从 start 逐级向上定位总仓根：同时包含 .git 与 go_projects 的目录。
// 兼容 worktree（.git 可为文件）。到盘符仍未命中则返回带 --root 用法提示的错误。
func FindRoot(start string) (string, error) {
	abs, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	cur := abs
	for {
		if isRootDir(cur) {
			return cur, nil
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", fmt.Errorf("未找到总仓根目录（需同时含 .git 与 go_projects）：%s；可用 --root <path> 显式指定", abs)
		}
		cur = parent
	}
}

func isRootDir(dir string) bool {
	gitPath := filepath.Join(dir, ".git")
	if _, err := os.Stat(gitPath); err != nil {
		return false
	}
	info, err := os.Stat(filepath.Join(dir, "go_projects"))
	return err == nil && info.IsDir()
}

// Discover 遍历 4 个语言子仓的直接子目录（仅目录，按名排序）生成 Entry 列表。
// 尚无 PROJECT.toml 的项目同样纳入，Meta 置 nil 表示未标注。
func Discover(root string) ([]Entry, error) {
	var entries []Entry
	for _, langDir := range langDirs {
		base := filepath.Join(root, langDir)
		dirs, err := os.ReadDir(base)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("read %s: %w", base, err)
		}
		lang := strings.TrimSuffix(langDir, "_projects")
		for _, d := range dirs {
			if !d.IsDir() {
				continue
			}
			dir := filepath.Join(langDir, d.Name()) // 相对根路径
			m, err := meta.Load(filepath.Join(root, dir))
			if err != nil {
				return nil, err
			}
			entries = append(entries, Entry{
				Dir:  filepath.ToSlash(dir),
				Name: d.Name(),
				Lang: lang,
				Meta: m,
			})
		}
	}
	return entries, nil
}

// CollectGit 以信号量 8 并发采集每个项目的最后提交时间与最近可达 tag。
// 每个项目仅执行只读 git 命令，不采集 commit message 等正文；
// 任一命令失败（git 不在 PATH / 非仓库 / 无 tag）该 Entry 保持零值，不报错不中断。
func CollectGit(root string, es []Entry) {
	const semSize = 8
	sem := make(chan struct{}, semSize)
	var wg sync.WaitGroup
	for i := range es {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			es[idx].Git = collectOne(filepath.Join(root, es[idx].Dir))
		}(i)
	}
	wg.Wait()
}

// collectOne 采集单个目录的 git 元数据；失败时返回零值 GitInfo。
func collectOne(dir string) GitInfo {
	var info GitInfo

	out, err := gitOut(dir, "log", "-1", "--format=%cI")
	if err == nil {
		if t, perr := time.Parse(time.RFC3339, strings.TrimSpace(out)); perr == nil {
			info.LastCommitAt = t
		}
	}

	hash, err := gitOut(dir, "log", "-1", "--format=%H")
	if err == nil {
		hash = strings.TrimSpace(hash)
		if hash != "" {
			if tag, terr := gitOut(dir, "describe", "--tags", "--abbrev=0", hash); terr == nil {
				info.Tag = strings.TrimSpace(tag)
			}
		}
	}
	return info
}

// gitOut 执行单条 git 命令并返回 stdout；任何失败（git 不在 PATH / 非仓库）均返回错误供调用方降级。
func gitOut(dir string, args ...string) (string, error) {
	full := append([]string{"-C", dir}, args...)
	out, err := exec.Command("git", full...).Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}
