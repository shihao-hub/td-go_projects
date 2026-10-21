package service

import (
	"clictl/internal/runner"
	"clictl/internal/store"
)

// Start 后台分离启动（DETACHED，点火即走）。前置失败（未注册/失效）返回
// 带 127 退出码的 *Error。
func (s *Service) Start(name string, args []string) (runner.StartResult, error) {
	res, err := runner.Start(s.st, store.NormalizeName(name), args)
	if err != nil {
		return runner.StartResult{}, runErrToService(err)
	}
	return res, nil
}

// StartConsole 在独立控制台窗口启动（命令行工具：用户可见、可交互）。
func (s *Service) StartConsole(name string, args []string) (runner.StartResult, error) {
	res, err := runner.StartConsole(s.st, store.NormalizeName(name), args)
	if err != nil {
		return runner.StartResult{}, runErrToService(err)
	}
	return res, nil
}

// StartWithMode 按显式模式启动；mode 为空时按 PE 子系统自动选择
// （console 程序走终端窗口，其余走后台分离）。
func (s *Service) StartWithMode(name string, args []string, mode string) (runner.StartResult, error) {
	switch mode {
	case runner.ModeConsole:
		return s.StartConsole(name, args)
	case runner.ModeDetached:
		return s.Start(name, args)
	case "":
		tool, err := s.PreflightRun(name)
		if err != nil {
			return runner.StartResult{}, err
		}
		if runner.IsConsoleExe(tool.Path) {
			return s.StartConsole(name, args)
		}
		return s.Start(name, args)
	default:
		return runner.StartResult{}, &Error{Code: "bad_args", Message: "mode 仅支持 console | detached", ExitCode: 1}
	}
}

// Stop 终止该工具全部后台活实例（树杀）并闭环记录。
// 部分失败（杀后复探仍存活）不算 error，由调用方按结果字段判定——CLI 层
// 据此返回退出码 1，MCP 层据此置 isError。
func (s *Service) Stop(name string) (runner.StopResult, error) {
	res, err := runner.Stop(s.st, store.NormalizeName(name))
	if err != nil {
		return runner.StopResult{}, runErrToService(err)
	}
	return res, nil
}

// PreflightRun run 类执行的前置校验：查注册 + 现场校验文件有效性。
// 供 MCP run 使用（CLI run 的前置校验在 runner.lookup，二者语义一致）。
func (s *Service) PreflightRun(name string) (store.Tool, error) {
	tool, err := s.st.GetTool(store.NormalizeName(name))
	if err != nil {
		return store.Tool{}, wrapErr("run", err)
	}
	if tool.Status != store.StatusActive {
		return store.Tool{}, &Error{Code: "invalid", Message: "文件已失效: " + tool.Path, ExitCode: 127}
	}
	return tool, nil
}
