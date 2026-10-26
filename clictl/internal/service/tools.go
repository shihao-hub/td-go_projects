package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"clictl/internal/runner"
	"clictl/internal/store"
)

// RunningInstance 一个存活的后台实例
type RunningInstance struct {
	PID       int    `json:"pid"`
	Target    string `json:"target,omitempty"`
	Mode      string `json:"mode,omitempty"`
	StartedAt string `json:"started_at"`
}

// RunningTool list --running 的输出形状：工具详情 + 活实例信息
type RunningTool struct {
	store.Tool
	RunningPIDs []int             `json:"running_pids,omitempty"`
	Instances   []RunningInstance `json:"instances,omitempty"`
	LastStartAt string            `json:"last_start,omitempty"` // 最新一条活记录的启动时间
}

// InfoResult info 的输出形状
type InfoResult struct {
	Tool            store.Tool     `json:"tool"`
	RecentLaunches  []store.Launch `json:"recent_launches"`
	FinishedCount   int64          `json:"finished_count"`
	TotalDurationMs int64          `json:"total_duration_ms"`
	Running         RunningState   `json:"running"`
	Console         bool           `json:"console"` // 默认目标是否为控制台程序
}

// RunningState 后台运行状态
type RunningState struct {
	Alive     bool              `json:"alive"`
	PIDs      []int             `json:"pids"`
	Instances []RunningInstance `json:"instances,omitempty"`
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

// RevealResult reveal 的输出形状
type RevealResult struct {
	Name   string `json:"name"`
	Target string `json:"target"`
	Path   string `json:"path"`
	IsDir  bool   `json:"is_dir"`
}

// ConsoleResult console show/hide 的输出形状
type ConsoleResult struct {
	Name    string `json:"name"`
	PID     int    `json:"pid"`
	Target  string `json:"target,omitempty"`
	Mode    string `json:"mode,omitempty"`
	Visible bool   `json:"visible"`
}

// CommandInput 源码命令输入
type CommandInput struct {
	Name    string          `json:"name,omitempty"`
	Program string          `json:"program"`
	Workdir string          `json:"workdir"`
	Args    []string        `json:"args,omitempty"`
	Type    string          `json:"type,omitempty"` // gui / console
	Desc    string          `json:"desc,omitempty"`
	Meta    json.RawMessage `json:"meta,omitempty"`
}

// CreateInput API 注册弹窗输入：exe 与源码可同时存在
type CreateInput struct {
	Name          string
	Description   string
	Meta          json.RawMessage
	DefaultTarget string
	ExePath       string
	Source        *CommandInput
}

// UpdateInput 全量编辑；Exe/Source 为 nil 表示删除该目标
type UpdateInput struct {
	Name          string
	Description   string
	Meta          json.RawMessage
	DefaultTarget string
	ExePath       *string
	Source        *CommandInput
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
	for i := range tools {
		fillToolConsole(&tools[i])
	}
	return tools, nil
}

// ListRunning 列出有后台活实例的工具。
func (s *Service) ListRunning() ([]RunningTool, error) {
	tools, err := s.st.ListTools("")
	if err != nil {
		return nil, wrapErr("list", err)
	}
	out := []RunningTool{}
	for _, t := range tools {
		fillToolConsole(&t)
		unfinished, err := s.st.UnfinishedStarts(t.ID)
		if err != nil {
			return nil, wrapErr("list", err)
		}
		alive := runner.FilterAliveForTool(unfinished, t)
		if len(alive) == 0 {
			continue
		}
		instances := buildInstances(t, alive)
		pids := make([]int, 0, len(instances))
		for _, inst := range instances {
			pids = append(pids, inst.PID)
		}
		out = append(out, RunningTool{
			Tool:        t,
			RunningPIDs: pids,
			Instances:   instances,
			LastStartAt: instances[len(instances)-1].StartedAt,
		})
	}
	return out, nil
}

// Info 工具详情 + 最近 10 条启动 + 累计耗时 + 后台运行状态
func (s *Service) Info(name string) (*InfoResult, error) {
	tool, err := s.st.GetTool(store.NormalizeName(name))
	if err != nil {
		return nil, wrapErr("info", err)
	}
	fillToolConsole(&tool)
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
	alive := runner.FilterAliveForTool(unfinished, tool)
	instances := buildInstances(tool, alive)
	pids := make([]int, 0, len(instances))
	for _, inst := range instances {
		pids = append(pids, inst.PID)
	}
	return &InfoResult{
		Tool:            tool,
		RecentLaunches:  launches,
		FinishedCount:   finished,
		TotalDurationMs: totalMs,
		Running:         RunningState{Alive: len(alive) > 0, PIDs: pids, Instances: instances},
		Console:         defaultConsole(tool),
	}, nil
}

// Add 注册 exe：校验 .exe 后缀、路径存在性、名字合法性。
// 同名卡片已有源码目标但没有 exe 时，把 exe 附加到该卡片。
func (s *Service) Add(path, name, desc string, meta json.RawMessage) (store.Tool, error) {
	abs, err := validateExePath(path)
	if err != nil {
		return store.Tool{}, err
	}

	finalName := store.NormalizeName(name)
	if finalName == "" {
		base := filepath.Base(abs)
		finalName = store.NormalizeName(strings.TrimSuffix(base, filepath.Ext(base)))
	}
	if finalName == "" || strings.ContainsAny(finalName, " \t/\\") {
		return store.Tool{}, &Error{Code: "bad_args", Message: "非法的 name: " + name, ExitCode: 1}
	}

	existing, gerr := s.st.GetTool(finalName)
	if gerr == nil {
		if existing.Exe != nil {
			return store.Tool{}, &Error{Code: "conflict", Message: "该 name 已被其他工具使用", ExitCode: 1}
		}
		tool, err := s.st.AttachExe(finalName, store.ExeSpec{Path: abs})
		if err != nil {
			return store.Tool{}, wrapErr("add", err)
		}
		fillToolConsole(&tool)
		return tool, nil
	}
	if !errors.Is(gerr, store.ErrNotFound) {
		return store.Tool{}, wrapErr("add", gerr)
	}

	var metaRaw []byte
	if len(meta) > 0 && strings.TrimSpace(string(meta)) != "" {
		metaRaw = []byte(meta)
	}
	tool, err := s.st.AddTool(store.AddSpec{
		Name:        finalName,
		Description: desc,
		Meta:        metaRaw,
		Exe:         &store.ExeSpec{Path: abs},
	})
	if err != nil {
		return store.Tool{}, wrapErr("add", err)
	}
	fillToolConsole(&tool)
	return tool, nil
}

// AddCommand 注册源码命令：同名卡片没有源码目标时附加，否则新建仅源码卡片。
func (s *Service) AddCommand(in CommandInput) (store.Tool, error) {
	source, err := s.normalizeSource(in)
	if err != nil {
		return store.Tool{}, err
	}
	name := store.NormalizeName(in.Name)
	if name == "" {
		name = deriveCommandName(source.Program, source.Args)
	}
	if name == "" || strings.ContainsAny(name, " \t/\\") {
		return store.Tool{}, &Error{Code: "bad_args", Message: "非法的 name: " + in.Name, ExitCode: 1}
	}

	existing, gerr := s.st.GetTool(name)
	if gerr == nil {
		if existing.Source != nil {
			return store.Tool{}, &Error{Code: "conflict", Message: "该工具的源码命令已存在，请用 edit 修改", ExitCode: 1}
		}
		tool, err := s.st.AttachSource(name, *source)
		if err != nil {
			return store.Tool{}, wrapErr("add-cmd", err)
		}
		fillToolConsole(&tool)
		return tool, nil
	}
	if !errors.Is(gerr, store.ErrNotFound) {
		return store.Tool{}, wrapErr("add-cmd", gerr)
	}

	var metaRaw []byte
	if len(in.Meta) > 0 && strings.TrimSpace(string(in.Meta)) != "" {
		metaRaw = []byte(in.Meta)
	}
	tool, err := s.st.AddTool(store.AddSpec{
		Name:        name,
		Description: in.Desc,
		Meta:        metaRaw,
		Source:      source,
	})
	if err != nil {
		return store.Tool{}, wrapErr("add-cmd", err)
	}
	fillToolConsole(&tool)
	return tool, nil
}

// Create API 注册弹窗：exe 与源码可同时创建。
func (s *Service) Create(in CreateInput) (store.Tool, error) {
	if in.ExePath == "" && in.Source == nil {
		return store.Tool{}, &Error{Code: "bad_args", Message: "exe 与源码命令至少填写一项", ExitCode: 1}
	}
	if in.ExePath == "" {
		src := *in.Source
		src.Name = in.Name
		src.Desc = in.Description
		src.Meta = in.Meta
		return s.AddCommand(src)
	}

	abs, err := validateExePath(in.ExePath)
	if err != nil {
		return store.Tool{}, err
	}
	if in.Source == nil {
		return s.Add(abs, in.Name, in.Description, in.Meta)
	}

	source, err := s.normalizeSource(*in.Source)
	if err != nil {
		return store.Tool{}, err
	}
	name := store.NormalizeName(in.Name)
	if name == "" {
		base := filepath.Base(abs)
		name = store.NormalizeName(strings.TrimSuffix(base, filepath.Ext(base)))
	}
	if name == "" || strings.ContainsAny(name, " \t/\\") {
		return store.Tool{}, &Error{Code: "bad_args", Message: "非法的 name: " + in.Name, ExitCode: 1}
	}
	var metaRaw []byte
	if len(in.Meta) > 0 && strings.TrimSpace(string(in.Meta)) != "" {
		metaRaw = []byte(in.Meta)
	}
	tool, err := s.st.AddTool(store.AddSpec{
		Name:          name,
		Description:   in.Description,
		Meta:          metaRaw,
		DefaultTarget: in.DefaultTarget,
		Exe:           &store.ExeSpec{Path: abs},
		Source:        source,
	})
	if err != nil {
		return store.Tool{}, wrapErr("add", err)
	}
	fillToolConsole(&tool)
	return tool, nil
}

// Update 全量编辑卡片。
func (s *Service) Update(in UpdateInput) (store.Tool, error) {
	var (
		exe    *store.ExeSpec
		source *store.SourceSpec
	)
	if in.ExePath != nil {
		abs, err := validateExePath(*in.ExePath)
		if err != nil {
			return store.Tool{}, err
		}
		exe = &store.ExeSpec{Path: abs}
	}
	if in.Source != nil {
		src, err := s.normalizeSource(*in.Source)
		if err != nil {
			return store.Tool{}, err
		}
		source = src
	}
	tool, err := s.st.UpdateTool(store.NormalizeName(in.Name), store.UpdateSpec{
		Description:   in.Description,
		Meta:          in.Meta,
		DefaultTarget: in.DefaultTarget,
		Exe:           exe,
		Source:        source,
	})
	if err != nil {
		return store.Tool{}, wrapErr("edit", err)
	}
	fillToolConsole(&tool)
	return tool, nil
}

// SetMeta 整体替换 meta（传 {} 清空），返回更新后的工具
func (s *Service) SetMeta(name string, meta json.RawMessage) (store.Tool, error) {
	tool, err := s.st.SetMeta(store.NormalizeName(name), meta)
	if err != nil {
		return store.Tool{}, wrapErr("set", err)
	}
	fillToolConsole(&tool)
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

// Cp 把已注册的 exe 复制到指定目录。仅 exe 目标可用。
func (s *Service) Cp(name, destDir string, force bool) (*CpResult, error) {
	tool, err := s.st.GetTool(store.NormalizeName(name))
	if err != nil {
		return nil, wrapErr("cp", err)
	}
	if tool.Exe == nil {
		return nil, &Error{Code: "no_exe_target", Message: "该工具没有 exe 目标，无法复制", ExitCode: 1}
	}
	fillToolConsole(&tool)
	if tool.Exe.Status != store.StatusActive {
		return nil, &Error{Code: "file_not_found", Message: "源文件已失效: " + tool.Exe.Path, ExitCode: 1}
	}

	if fi, err := os.Stat(destDir); err != nil || !fi.IsDir() {
		return nil, &Error{Code: "dest_not_found", Message: "目标目录不存在: " + destDir, ExitCode: 1}
	}

	dest := filepath.Join(destDir, filepath.Base(tool.Exe.Path))
	if destFi, err := os.Stat(dest); err == nil {
		// 源=目标（同文件/同路径）时复制会截断损坏源文件，force 也不允许
		if srcFi, err := os.Stat(tool.Exe.Path); err == nil && os.SameFile(srcFi, destFi) {
			return nil, &Error{Code: "same_path", Message: "源与目标是同一文件: " + dest, ExitCode: 1}
		}
		if !force {
			return nil, &Error{Code: "dest_exists", Message: "目标已存在: " + dest + "（覆盖需确认）", ExitCode: 1}
		}
	}

	if err := copyFile(tool.Exe.Path, dest); err != nil {
		return nil, &Error{Code: "copy_failed", Message: err.Error(), ExitCode: 1}
	}
	return &CpResult{
		Name:      tool.Name,
		Src:       tool.Exe.Path,
		Dest:      dest,
		SizeBytes: tool.Exe.SizeBytes,
	}, nil
}

// Reveal 返回资源管理器定位目标；exe → 文件，源码 → 工作目录。
func (s *Service) Reveal(name, target string) (*RevealResult, error) {
	tool, err := s.st.GetTool(store.NormalizeName(name))
	if err != nil {
		return nil, wrapErr("reveal", err)
	}
	if target == "" {
		target = tool.DefaultTarget
	}
	switch target {
	case store.TargetExe:
		if tool.Exe == nil {
			return nil, &Error{Code: "no_exe_target", Message: "该工具没有 exe 目标", ExitCode: 1}
		}
		if tool.Exe.Status != store.StatusActive {
			return nil, &Error{Code: "file_not_found", Message: "exe 文件已失效: " + tool.Exe.Path, ExitCode: 1}
		}
		return &RevealResult{Name: tool.Name, Target: target, Path: tool.Exe.Path}, nil
	case store.TargetSource:
		if tool.Source == nil {
			return nil, &Error{Code: "no_source_target", Message: "该工具没有源码命令目标", ExitCode: 1}
		}
		if tool.Source.Status != store.StatusActive {
			return nil, &Error{Code: "file_not_found", Message: "源码工作目录不存在: " + tool.Source.Workdir, ExitCode: 1}
		}
		return &RevealResult{Name: tool.Name, Target: target, Path: tool.Source.Workdir, IsDir: true}, nil
	default:
		return nil, &Error{Code: "bad_args", Message: "target 仅支持 exe | source", ExitCode: 1}
	}
}

// SetConsoleVisible 显示/隐藏某个存活实例的控制台；pid=0 时取最新实例。
func (s *Service) SetConsoleVisible(name string, pid int, visible bool) (*ConsoleResult, error) {
	tool, err := s.st.GetTool(store.NormalizeName(name))
	if err != nil {
		return nil, wrapErr("console", err)
	}
	unfinished, err := s.st.UnfinishedStarts(tool.ID)
	if err != nil {
		return nil, wrapErr("console", err)
	}
	alive := runner.FilterAliveForTool(unfinished, tool)
	if len(alive) == 0 {
		return nil, &Error{Code: "not_running", Message: "该工具没有存活的后台实例", ExitCode: 1}
	}

	var launch *store.Launch
	if pid > 0 {
		for i := range alive {
			if alive[i].PID != nil && *alive[i].PID == pid {
				launch = &alive[i]
				break
			}
		}
		if launch == nil {
			return nil, &Error{Code: "not_running", Message: fmt.Sprintf("PID %d 不是该工具的存活实例", pid), ExitCode: 1}
		}
	} else {
		launch = &alive[len(alive)-1]
	}

	if !consoleSwitchable(tool, *launch) {
		return nil, &Error{Code: "console_unavailable", Message: "该实例没有可显示/隐藏的控制台", ExitCode: 1}
	}
	if err := runner.SetConsoleVisible(*launch.PID, visible); err != nil {
		return nil, &Error{Code: "console_failed", Message: err.Error(), ExitCode: 1}
	}
	return &ConsoleResult{
		Name:    tool.Name,
		PID:     *launch.PID,
		Target:  launch.Target,
		Mode:    launch.Mode,
		Visible: visible,
	}, nil
}

func buildInstances(tool store.Tool, launches []store.Launch) []RunningInstance {
	instances := make([]RunningInstance, 0, len(launches))
	for _, l := range launches {
		target := l.Target
		if target == "" {
			target = tool.DefaultTarget
		}
		instances = append(instances, RunningInstance{
			PID:       *l.PID,
			Target:    target,
			Mode:      l.Mode,
			StartedAt: l.StartedAt,
		})
	}
	return instances
}

func fillToolConsole(t *store.Tool) {
	if t.Exe != nil {
		t.Exe.Console = runner.IsConsoleExe(t.Exe.Path)
	}
}

func defaultConsole(tool store.Tool) bool {
	switch tool.DefaultTarget {
	case store.TargetExe:
		if tool.Exe != nil {
			return tool.Exe.Console
		}
	case store.TargetSource:
		if tool.Source != nil {
			return tool.Source.Type == store.SourceTypeConsole
		}
	}
	return false
}

func consoleSwitchable(tool store.Tool, l store.Launch) bool {
	switch l.Mode {
	case store.ModeConsole, store.ModeHidden:
		return true
	case "":
		switch l.Target {
		case store.TargetExe:
			return tool.Exe != nil && runner.IsConsoleExe(tool.Exe.Path)
		case store.TargetSource:
			return tool.Source != nil && tool.Source.Type == store.SourceTypeConsole
		}
	}
	return false
}

func validateExePath(path string) (string, error) {
	if strings.ToLower(filepath.Ext(path)) != ".exe" {
		return "", &Error{Code: "not_exe", Message: "仅支持 .exe 文件: " + path, ExitCode: 1}
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", &Error{Code: "bad_args", Message: "路径无效: " + err.Error(), ExitCode: 1}
	}
	abs = filepath.Clean(abs)
	if fi, err := os.Stat(abs); err != nil || fi.IsDir() {
		return "", &Error{Code: "file_not_found", Message: "文件不存在: " + abs, ExitCode: 1}
	}
	return abs, nil
}

func (s *Service) normalizeSource(in CommandInput) (*store.SourceSpec, error) {
	program := strings.TrimSpace(in.Program)
	if program == "" {
		return nil, &Error{Code: "bad_args", Message: "源码程序不能为空", ExitCode: 1}
	}
	if !filepath.IsAbs(program) {
		resolved, err := exec.LookPath(program)
		if err != nil {
			return nil, &Error{Code: "program_not_found", Message: "找不到程序: " + program + "（可填写绝对路径，如 C:\\Users\\...\\.local\\bin\\uv.exe）", ExitCode: 1}
		}
		program = resolved
	}
	abs, err := filepath.Abs(program)
	if err != nil {
		return nil, &Error{Code: "bad_args", Message: "解析程序路径失败: " + err.Error(), ExitCode: 1}
	}
	abs = filepath.Clean(abs)
	if strings.ToLower(filepath.Ext(abs)) != ".exe" {
		return nil, &Error{Code: "not_exe", Message: "源码启动程序必须是 .exe: " + abs, ExitCode: 1}
	}
	if fi, err := os.Stat(abs); err != nil || fi.IsDir() {
		return nil, &Error{Code: "program_not_found", Message: "程序不存在: " + abs, ExitCode: 1}
	}

	workAbs, err := filepath.Abs(strings.TrimSpace(in.Workdir))
	if err != nil {
		return nil, &Error{Code: "bad_args", Message: "解析工作目录失败: " + err.Error(), ExitCode: 1}
	}
	workAbs = filepath.Clean(workAbs)
	if fi, err := os.Stat(workAbs); err != nil || !fi.IsDir() {
		return nil, &Error{Code: "workdir_not_found", Message: "工作目录不存在: " + workAbs, ExitCode: 1}
	}

	typ := strings.ToLower(strings.TrimSpace(in.Type))
	if typ == "" {
		typ = store.SourceTypeGUI
	}
	if typ != store.SourceTypeGUI && typ != store.SourceTypeConsole {
		return nil, &Error{Code: "bad_args", Message: "源码类型仅支持 gui | console", ExitCode: 1}
	}
	return &store.SourceSpec{Program: abs, Workdir: workAbs, Args: in.Args, Type: typ}, nil
}

func deriveCommandName(program string, args []string) string {
	for i := len(args) - 1; i >= 0; i-- {
		a := strings.TrimSpace(args[i])
		if a == "" || strings.HasPrefix(a, "-") {
			continue
		}
		base := filepath.Base(a)
		return store.NormalizeName(strings.TrimSuffix(base, filepath.Ext(base)))
	}
	base := filepath.Base(program)
	return store.NormalizeName(strings.TrimSuffix(base, filepath.Ext(base)))
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
