package cli

import (
	"encoding/json"
	"fmt"
	"os"
)

type envelope struct {
	OK    bool       `json:"ok"`
	Data  any        `json:"data,omitempty"`
	Error *errorBody `json:"error,omitempty"`
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func emitJSON(exitCode int, data any) int {
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(envelope{OK: true, Data: data}); err != nil {
		fmt.Fprintln(os.Stderr, "typeai: JSON 输出失败:", err)
		return 1
	}
	return exitCode
}

func emitJSONError(code, message string) int {
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(envelope{OK: false, Error: &errorBody{Code: code, Message: message}}); err != nil {
		fmt.Fprintln(os.Stderr, "typeai: JSON 错误输出失败:", err)
		return 1
	}
	if code == "bad_args" {
		return 2
	}
	return 1
}

func onlyJSONFlag(args []string) (bool, error) {
	jsonMode := false
	for _, arg := range args {
		if arg == "--json" {
			jsonMode = true
			continue
		}
		return false, fmt.Errorf("不支持参数 %s", arg)
	}
	return jsonMode, nil
}
