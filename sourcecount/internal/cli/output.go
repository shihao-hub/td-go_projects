package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"sourcecount/internal/contract"
	"sourcecount/internal/service"
)

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type envelope struct {
	OK    bool                 `json:"ok"`
	Data  *contract.ScanReport `json:"data,omitempty"`
	Error *errorBody           `json:"error,omitempty"`
}

func writeJSON(w io.Writer, value any, pretty bool) error {
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	if pretty {
		encoder.SetIndent("", "  ")
	}
	return encoder.Encode(value)
}

func emitReport(report contract.ScanReport, jsonMode, pretty bool) int {
	if jsonMode {
		if len(report.Errors) > 0 {
			_ = writeJSON(os.Stdout, envelope{
				OK:    false,
				Data:  &report,
				Error: &errorBody{Code: "partial_scan", Message: fmt.Sprintf("扫描完成，但有 %d 个文件未能统计", len(report.Errors))},
			}, pretty)
			return 1
		}
		_ = writeJSON(os.Stdout, envelope{OK: true, Data: &report}, pretty)
		return 0
	}
	printHumanReport(os.Stdout, report)
	if len(report.Errors) > 0 {
		fmt.Fprintf(os.Stderr, "警告：%d 个文件未能统计\n", len(report.Errors))
		for _, scanErr := range report.Errors {
			fmt.Fprintf(os.Stderr, "  %s: %s (%s)\n", scanErr.Path, scanErr.Message, scanErr.Code)
		}
		return 1
	}
	return 0
}

func emitServiceError(err error, jsonMode, pretty bool) int {
	code, message := "internal", err.Error()
	var serviceErr *service.Error
	if errors.As(err, &serviceErr) {
		code, message = serviceErr.Code, serviceErr.Message
	}
	if jsonMode {
		_ = writeJSON(os.Stdout, envelope{OK: false, Error: &errorBody{Code: code, Message: message}}, pretty)
	} else {
		fmt.Fprintln(os.Stderr, "错误:", code+":", message)
	}
	if code == "bad_args" || code == "bad_config" || code == "config_not_found" || code == "config_read_failed" || code == "config_discovery_failed" || code == "root_not_found" {
		return 2
	}
	return 1
}

func printHumanReport(w io.Writer, report contract.ScanReport) {
	if len(report.Roots) > 0 {
		fmt.Fprintf(w, "扫描根路径: %s\n", strings.Join(report.Roots, ", "))
	}
	if report.ConfigPath != "" {
		fmt.Fprintf(w, "配置文件: %s\n", report.ConfigPath)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "后缀\t文本文件\t文本行数\t文本字符\t二进制文件\t二进制字节")
	for _, group := range report.Groups {
		fmt.Fprintf(w, "%s\t%d\t%d\t%d\t%d\t%d\n", group.Extension, group.Text.Files, group.Text.Lines, group.Text.Chars, group.Binary.Files, group.Binary.Bytes)
	}
	fmt.Fprintf(w, "总计\t%d\t%d\t%d\t%d\t%d\n", report.Totals.Text.Files, report.Totals.Text.Lines, report.Totals.Text.Chars, report.Totals.Binary.Files, report.Totals.Binary.Bytes)
}
