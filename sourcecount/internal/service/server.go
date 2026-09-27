// Package service 是 sourcecount 的公共 Server/Service 层。CLI 与 MCP 都只
// 通过该层执行扫描，不在适配器中重复业务编排。
package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"sourcecount/internal/config"
	"sourcecount/internal/contract"
	"sourcecount/internal/scanner"
)

// Server 是无状态的公共业务服务。
type Server struct{}

// New 创建公共 Server。
func New() *Server { return &Server{} }

// Error 是跨 CLI/MCP 的稳定业务错误，不携带入口专属退出码。
type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

// Scan 执行一次完整扫描。单文件问题进入 report.Errors；请求级问题返回 Error。
func (s *Server) Scan(ctx context.Context, req contract.ScanRequest) (contract.ScanReport, error) {
	roots, err := normalizeRoots(req.Roots)
	if err != nil {
		return contract.ScanReport{}, err
	}

	effective, configPath, err := config.Resolve(roots, config.Overrides{
		ConfigPath:        req.ConfigPath,
		Include:           req.Include,
		Exclude:           req.Exclude,
		TextExtensions:    req.TextExtensions,
		BinaryExtensions:  req.BinaryExtensions,
		NoDefaultExcludes: req.NoDefaultExcludes,
	})
	if err != nil {
		return contract.ScanReport{}, &Error{Code: config.ErrorCode(err), Message: err.Error()}
	}
	matcher, err := config.NewMatcher(effective.Include, effective.Exclude)
	if err != nil {
		return contract.ScanReport{}, &Error{Code: "bad_config", Message: err.Error()}
	}

	results, err := scanner.Scan(ctx, scanner.Options{
		Roots:            roots,
		Matcher:          matcher,
		TextExtensions:   effective.TextExtensions,
		BinaryExtensions: effective.BinaryExtensions,
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return contract.ScanReport{}, &Error{Code: "cancelled", Message: "扫描已取消"}
		}
		return contract.ScanReport{}, &Error{Code: "scan_failed", Message: err.Error()}
	}
	return aggregate(roots, configPath, results), nil
}

func normalizeRoots(raw []string) ([]string, error) {
	if len(raw) == 0 {
		raw = []string{"."}
	}
	seen := make(map[string]struct{}, len(raw))
	roots := make([]string, 0, len(raw))
	for _, value := range raw {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, &Error{Code: "bad_args", Message: "扫描根路径不能是空字符串"}
		}
		abs, err := filepath.Abs(value)
		if err != nil {
			return nil, &Error{Code: "bad_args", Message: "根路径无效: " + err.Error()}
		}
		abs = filepath.Clean(abs)
		if _, err := os.Lstat(abs); err != nil {
			return nil, &Error{Code: "root_not_found", Message: "根路径不存在或不可访问: " + abs}
		}
		key := abs
		if runtime.GOOS == "windows" {
			key = strings.ToLower(key)
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		roots = append(roots, abs)
	}
	sort.Strings(roots)
	return roots, nil
}
