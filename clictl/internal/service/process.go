package service

import (
	"clictl/internal/runner"
	"clictl/internal/store"
)

// Start 后台分离启动（DETACHED，无控制台）。
func (s *Service) Start(name, target string, args []string) (runner.StartResult, error) {
	res, err := runner.Start(s.st, store.NormalizeName(name), target, args)
	if err != nil {
		return runner.StartResult{}, runErrToService(err)
	}
	return res, nil
}

// StartConsole 在独立控制台窗口启动（可见）。
func (s *Service) StartConsole(name, target string, args []string) (runner.StartResult, error) {
	res, err := runner.StartConsole(s.st, store.NormalizeName(name), target, args)
	if err != nil {
		return runner.StartResult{}, runErrToService(err)
	}
	return res, nil
}

// StartHiddenConsole 在隐藏控制台启动（可随时显示/隐藏）。
func (s *Service) StartHiddenConsole(name, target string, args []string) (runner.StartResult, error) {
	res, err := runner.StartHiddenConsole(s.st, store.NormalizeName(name), target, args)
	if err != nil {
		return runner.StartResult{}, runErrToService(err)
	}
	return res, nil
}

// StartWithMode 按显式模式启动；mode 为空时按目标类型自动选择。
func (s *Service) StartWithMode(name, target string, args []string, mode string) (runner.StartResult, error) {
	res, err := runner.StartWithMode(s.st, store.NormalizeName(name), target, mode, args)
	if err != nil {
		return runner.StartResult{}, runErrToService(err)
	}
	return res, nil
}

// Stop 终止该工具全部后台活实例（树杀）并闭环记录。
func (s *Service) Stop(name string) (runner.StopResult, error) {
	res, err := runner.Stop(s.st, store.NormalizeName(name))
	if err != nil {
		return runner.StopResult{}, runErrToService(err)
	}
	return res, nil
}

// Resolve 解析一次 run 的目标与启动命令（MCP 非交互执行复用）。
func (s *Service) Resolve(name, target string, extraArgs []string) (store.Tool, runner.LaunchSpec, error) {
	tool, err := s.st.GetTool(store.NormalizeName(name))
	if err != nil {
		return store.Tool{}, runner.LaunchSpec{}, wrapErr("run", err)
	}
	fillToolConsole(&tool)
	spec, rerr := runner.BuildSpec(tool, target, extraArgs)
	if rerr != nil {
		return store.Tool{}, runner.LaunchSpec{}, runErrToService(rerr)
	}
	return tool, spec, nil
}

// PreflightRun run 类执行的前置校验：查注册 + 默认目标有效性。
func (s *Service) PreflightRun(name string) (store.Tool, error) {
	tool, _, err := s.Resolve(name, "", nil)
	return tool, err
}
