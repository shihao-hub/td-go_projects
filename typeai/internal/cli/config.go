package cli

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"typeai/internal/config"
	"typeai/internal/service"
)

func cmdConfig(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "typeai: 用法: typeai config get|set")
		return 2
	}
	switch args[0] {
	case "get":
		return configGet(args[1:])
	case "set":
		return configSet(args[1:])
	default:
		fmt.Fprintln(os.Stderr, "typeai: 未知 config 子命令:", args[0])
		return 2
	}
}

func configGet(args []string) int {
	jsonMode, err := onlyJSONFlag(args)
	if err != nil {
		return emitConfigError(badArg(err), jsonMode, 2)
	}
	dir, err := config.DefaultDir()
	if err != nil {
		return emitConfigError(err, jsonMode, 1)
	}
	cfg, err := config.Load(dir)
	if err != nil {
		return emitConfigError(err, jsonMode, 1)
	}
	data := map[string]any{
		"base_url":      cfg.BaseURL,
		"model":         cfg.Model,
		"api_key_set":   cfg.APIKey != "",
		"config_path":   config.Path(dir),
		"env_overrides": environmentOverrides(),
	}
	if jsonMode {
		return emitJSON(0, data)
	}
	fmt.Printf("base_url: %s\n", cfg.BaseURL)
	fmt.Printf("model: %s\n", cfg.Model)
	fmt.Printf("api_key_set: %t\n", cfg.APIKey != "")
	fmt.Printf("config_path: %s\n", config.Path(dir))
	fmt.Printf("env_overrides: %s\n", strings.Join(environmentOverrides(), ","))
	return 0
}

func configSet(args []string) int {
	dir, err := config.DefaultDir()
	if err != nil {
		return emitConfigError(err, false, 1)
	}
	disk, err := config.LoadDisk(dir)
	if err != nil {
		return emitConfigError(err, false, 1)
	}
	// 这里读取的是磁盘值，避免环境变量在部分更新时被意外固化到配置文件。
	fs := flag.NewFlagSet("typeai config set", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	baseURL := fs.String("base-url", disk.BaseURL, "OpenAI 兼容 API 地址")
	apiKey := fs.String("api-key", disk.APIKey, "API Key")
	model := fs.String("model", disk.Model, "模型名")
	jsonMode := fs.Bool("json", false, "输出 JSON 包络")
	if err := fs.Parse(args); err != nil {
		return emitConfigError(badArg(err), *jsonMode, 2)
	}
	if fs.NArg() != 0 {
		return emitConfigError(badArg(fmt.Errorf("不支持位置参数 %s", fs.Arg(0))), *jsonMode, 2)
	}

	saved := config.Config{BaseURL: *baseURL, APIKey: *apiKey, Model: *model}
	if err := config.Save(dir, saved); err != nil {
		return emitConfigError(err, *jsonMode, 1)
	}
	data := map[string]any{
		"base_url":    saved.BaseURL,
		"model":       saved.Model,
		"api_key_set": saved.APIKey != "",
		"config_path": config.Path(dir),
	}
	if *jsonMode {
		return emitJSON(0, data)
	}
	fmt.Printf("base_url: %s\n", saved.BaseURL)
	fmt.Printf("model: %s\n", saved.Model)
	fmt.Printf("api_key_set: %t\n", saved.APIKey != "")
	fmt.Printf("config_path: %s\n", config.Path(dir))
	return 0
}

func environmentOverrides() []string {
	overrides := make([]string, 0, 3)
	for _, name := range []string{"TYPEAI_BASE_URL", "TYPEAI_API_KEY", "TYPEAI_MODEL"} {
		if strings.TrimSpace(os.Getenv(name)) != "" {
			overrides = append(overrides, name)
		}
	}
	return overrides
}

func emitConfigError(err error, jsonMode bool, exitCode int) int {
	if jsonMode {
		var coded *service.OperationError
		if errors.As(err, &coded) {
			return emitJSONError(coded.Code, coded.Message)
		}
		var validation *config.ValidationError
		if errors.As(err, &validation) {
			return emitJSONError("bad_args", validation.Message)
		}
		return emitJSONError("internal", err.Error())
	}
	fmt.Fprintln(os.Stderr, "typeai:", err)
	return exitCode
}

func badArg(err error) error {
	return &config.ValidationError{Message: err.Error()}
}
