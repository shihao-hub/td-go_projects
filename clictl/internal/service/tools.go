package service

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"clictl/internal/runner"
	"clictl/internal/store"
)

// RunningTool list --running 的输出形状：工具详情 + 活实例信息
type RunningTool struct {
	store.Tool
	RunningPIDs []int  `json:"running_pids,omitempty"`
	LastStartAt string `json:"last_start,omitempty"` // 最新一条活记录的启动时间
}

// InfoResult info 的输出形状
type InfoResult struct {
	Tool            store.Tool     `json:"tool"`
	RecentLaunches  []store.Launch `json:"recent_launches"`
	FinishedCount   int64          `json:"finished_count"`
	TotalDurationMs int64          `json:"total_duration_ms"`
	Running         RunningState   `json:"running"`
}

// RunningState 后台运行状态
type RunningState struct {
	Alive bool  `json:"alive"`
	PIDs  []int `json:"pids"`
}

// RemoveResult rm 的输出形状
type RemoveResult struct {
	Removed int64  `json:"removed"`
	Name    string `json:"name"`
}

// CpResult cp 的输出形状
type CpResult struct {
	Name      string `json:"name"`
	Src       string `json:"src"`
	Dest      string `json:"dest"`
	SizeBytes int64  `json:"size_bytes"`
}

// ListTools 列出工具。status 空串 = 不过滤（枚举校验：active | invalid）。
func (s *Service) ListTools(status string) ([]store.Tool, error) {
	if status != "" && status != store.StatusActive && status != store.StatusInvalid {
		return nil, &Error{Code: "bad_args", Message: "status 仅支持 active | invalid", ExitCode: 1}
	}
	tools, err := s.st.ListTools(status)
	if err != nil {
		return nil, wrapErr("list", err)
	}
	return tools, nil
}

// ListRunning 列出有后台活实例的工具（逐工具探活，开销为每工具一次
// UnfinishedStarts 查询 + 数次 Win32 探活）。
func (s *Service) ListRunning() ([]RunningTool, error) {
	tools, err := s.st.ListTools("")
	if err != nil {
		return nil, wrapErr("list", err)
	}
	out := []RunningTool{}
	for _, t := range tools {
		unfinished, err := s.st.UnfinishedStarts(t.ID)
		if err != nil {
			return nil, wrapErr("list", err)
		}
		alive := runner.FilterAlive(unfinished, t.Path)
		if len(alive) == 0 {
			continue
		}
		pids := make([]int, 0, len(alive))
		for _, l := range alive {
			pids = append(pids, *l.PID)
		}
		out = append(out, RunningTool{Tool: t, RunningPIDs: pids, LastStartAt: alive[len(alive)-1].StartedAt})
	}
	return out, nil
}

// Info 工具详情 + 最近 10 条启动 + 累计耗时 + 后台运行状态
func (s *Service) Info(name string) (*InfoResult, error) {
	tool, err := s.st.GetTool(store.NormalizeName(name))
	if err != nil {
		return nil, wrapErr("info", err)
	}
	launches, err := s.st.RecentLaunches(tool.ID, 10)
	if err != nil {
		return nil, wrapErr("info", err)
	}
	finished, totalMs, err := s.st.TotalDuration(tool.ID)
	if err != nil {
		return nil, wrapErr("info", err)
	}
	unfinished, err := s.st.UnfinishedStarts(tool.ID)
	if err != nil {
		return nil, wrapErr("info", err)
	}
	alive := runner.FilterAlive(unfinished, tool.Path)
	runningPIDs := make([]int, 0, len(alive))
	for _, l := range alive {
		runningPIDs = append(runningPIDs, *l.PID)
	}
	return &InfoResult{
		Tool:            tool,
		RecentLaunches:  launches,
		FinishedCount:   finished,
		TotalDurationMs: totalMs,
		Running:         RunningState{Alive: len(alive) > 0, PIDs: runningPIDs},
	}, nil
}

// Add 注册 exe：校验 .exe 后缀、路径存在性、名字合法性（name 空 = 文件名去
// .exe 小写化）。meta 为空/nil 时存 NULL。
func (s *Service) Add(path, name, desc string, meta json.RawMessage) (store.Tool, error) {
	// v1 仅管理 .exe（.cmd/.bat shim 二期）
	if strings.ToLower(filepath.Ext(path)) != ".exe" {
		return store.Tool{}, &Error{Code: "not_exe", Message: "v1 仅支持注册 .exe 文件: " + path, ExitCode: 1}
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return store.Tool{}, &Error{Code: "bad_args", Message: "路径无效: " + err.Error(), ExitCode: 1}
	}
	if fi, err := os.Stat(abs); err != nil || fi.IsDir() {
		return store.Tool{}, &Error{Code: "file_not_found", Message: "文件不存在: " + abs, ExitCode: 1}
	}

	finalName := store.NormalizeName(name)
	if finalName == "" {
		base := filepath.Base(abs)
		finalName = store.NormalizeName(strings.TrimSuffix(base, filepath.Ext(base)))
	}
	if finalName == "" || strings.ContainsAny(finalName, " \t/\\") {
		return store.Tool{}, &Error{Code: "bad_args", Message: "非法的 name: " + name, ExitCode: 1}
	}

	var metaRaw []byte
	if len(meta) > 0 && strings.TrimSpace(string(meta)) != "" {
		metaRaw = []byte(meta)
	}

	tool, err := s.st.AddTool(finalName, abs, desc, metaRaw)
	if err != nil {
		return store.Tool{}, wrapErr("add", err)
	}
	return tool, nil
}

// SetMeta 整体替换 meta（传 {} 清空），返回更新后的工具
func (s *Service) SetMeta(name string, meta json.RawMessage) (store.Tool, error) {
	tool, err := s.st.SetMeta(store.NormalizeName(name), meta)
	if err != nil {
		return store.Tool{}, wrapErr("set", err)
	}
	return tool, nil
}

// Remove 删除注册（外键级联删其 launches）
func (s *Service) Remove(name string) (*RemoveResult, error) {
	norm := store.NormalizeName(name)
	n, err := s.st.RemoveTool(norm)
	if err != nil {
		return nil, wrapErr("rm", err)
	}
	return &RemoveResult{Removed: n, Name: norm}, nil
}

// Cp 把已注册的 exe 复制到指定目录。目标目录必须已存在（不自动创建）；
// 目标同名文件默认拒绝覆盖，force 强制；源=目标时 force 也不允许（会截断损坏源文件）。
func (s *Service) Cp(name, destDir string, force bool) (*CpResult, error) {
	tool, err := s.st.GetTool(store.NormalizeName(name))
	if err != nil {
		return nil, wrapErr("cp", err)
	}
	if tool.Status != store.StatusActive {
		return nil, &Error{Code: "file_not_found", Message: "源文件已失效: " + tool.Path, ExitCode: 1}
	}

	if fi, err := os.Stat(destDir); err != nil || !fi.IsDir() {
		return nil, &Error{Code: "dest_not_found", Message: "目标目录不存在: " + destDir, ExitCode: 1}
	}

	dest := filepath.Join(destDir, filepath.Base(tool.Path))
	if destFi, err := os.Stat(dest); err == nil {
		// 源=目标（同文件/同路径）时复制会截断损坏源文件，force 也不允许
		if srcFi, err := os.Stat(tool.Path); err == nil && os.SameFile(srcFi, destFi) {
			return nil, &Error{Code: "same_path", Message: "源与目标是同一文件: " + dest, ExitCode: 1}
		}
		if !force {
			return nil, &Error{Code: "dest_exists", Message: "目标已存在: " + dest + "（覆盖需确认）", ExitCode: 1}
		}
	}

	if err := copyFile(tool.Path, dest); err != nil {
		return nil, &Error{Code: "copy_failed", Message: err.Error(), ExitCode: 1}
	}
	return &CpResult{
		Name:      tool.Name,
		Src:       tool.Path,
		Dest:      dest,
		SizeBytes: tool.SizeBytes,
	}, nil
}

// copyFile 流式复制文件内容（exe 可达数百 MB，不整读进内存）；
// 显式 Close 捕获落盘错误，defer Close 仅兜底异常路径
func copyFile(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("打开源文件失败: %w", err)
	}
	defer in.Close()
	out, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("创建目标文件失败: %w", err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return fmt.Errorf("复制内容失败: %w", err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("写入目标文件失败: %w", err)
	}
	return nil
}
