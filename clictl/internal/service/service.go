// Package service 承载 clictl 的业务逻辑：参数校验 + store/runner 调用，
// 返回 (data, error)。不做任何输出与 os.Exit——CLI 包络层与 MCP 适配层共用。
package service

import (
	"errors"

	"clictl/internal/runner"
	"clictl/internal/store"
)

// Error 业务错误：Code 即 JSON 错误码；ExitCode 为 CLI 退出码（0 表示未指定，
// 调用方按命令类型取默认值 1）；Suggestions 为未注册工具的相似名建议（可选）。
type Error struct {
	Code        string
	Message     string
	ExitCode    int
	Suggestions []string
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

// Service 业务服务：长持一个 SQLite 连接。
// CLI 每命令一进程即用即弃；MCP server 常驻复用。
type Service struct {
	st *store.Store
}

// Open 打开数据库并完成迁移
func Open() (*Service, error) {
	st, err := store.Open()
	if err != nil {
		return nil, err
	}
	return &Service{st: st}, nil
}

// Close 关闭数据库
func (s *Service) Close() error { return s.st.Close() }

// Store 暴露底层存储（runner.Run 前台透传等仍直接操作 store 的场景）
func (s *Service) Store() *store.Store { return s.st }

// Names 全部工具名（补全脚本消费，按 ListTools 默认排序）
func (s *Service) Names() ([]string, error) {
	tools, err := s.st.ListTools("")
	if err != nil {
		return nil, wrapErr("completion", err)
	}
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		names = append(names, t.Name)
	}
	return names, nil
}

// wrapErr 把 store 领域错误统一映射为 *Error（对应原 cli.failFromErr）
func wrapErr(prefix string, err error) error {
	var metaErr *store.MetaError
	if errors.As(err, &metaErr) {
		return &Error{Code: metaErr.Code, Message: metaErr.Message, ExitCode: 1}
	}
	var confErr *store.ConflictError
	if errors.As(err, &confErr) {
		switch confErr.Field {
		case "name":
			return &Error{Code: "conflict", Message: "该 name 已被其他工具使用", ExitCode: 1}
		case "path":
			return &Error{Code: "conflict", Message: "该 exe 路径已注册为其他工具", ExitCode: 1}
		default:
			return &Error{Code: "conflict", Message: "唯一性冲突: " + confErr.Field, ExitCode: 1}
		}
	}
	if errors.Is(err, store.ErrNotFound) {
		return &Error{Code: "not_found", Message: prefix + ": 未找到该工具", ExitCode: 1}
	}
	return &Error{Code: "internal", Message: prefix + ": " + err.Error(), ExitCode: 1}
}

// runErrToService 把 runner.RunError（run/start/stop 前置失败）映射为 *Error，
// 保留退出码与相似名建议
func runErrToService(err error) error {
	var re *runner.RunError
	if errors.As(err, &re) {
		return &Error{Code: re.Code, Message: re.Message, ExitCode: re.ExitCode, Suggestions: re.Suggestions}
	}
	return &Error{Code: "internal", Message: err.Error(), ExitCode: 1}
}
