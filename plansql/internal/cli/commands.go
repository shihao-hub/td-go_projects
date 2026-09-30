package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"plansql/internal/buildinfo"
	"plansql/internal/daemon"
	"runtime"
	"strings"
	"text/tabwriter"
	"time"
)

// PrintJSON 将对象以缩进的 JSON 格式输出
func PrintJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

// RunCLI 执行 CLI 命令分发
func RunCLI(args []string) int {
	if len(args) == 0 {
		printUsage()
		return 0
	}

	cmd := args[0]
	subArgs := args[1:]

	switch cmd {
	case "serve":
		return runServe(subArgs)
	case "list":
		return runList(subArgs)
	case "check":
		return runCheck(subArgs)
	case "set":
		return runSet(subArgs)
	case "scan":
		return runScan(subArgs)
	case "ui":
		return runUI(subArgs)
	case "schema":
		printFullSchema()
		return 0
	case "help", "--help", "-h":
		printUsage()
		return 0
	case "version", "--version", "-v":
		fmt.Printf("plansql %s (build: %s, Go 1.25.3, standard v2 daemon architecture)\n", buildinfo.Version, buildinfo.BuildID)
		return 0
	default:
		// 支持 plansql --schema 全局导出
		if cmd == "--schema" {
			printFullSchema()
			return 0
		}
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", cmd)
		printUsage()
		return 1
	}
}

func printUsage() {
	fmt.Println(`plansql - 基于 SQL 追加流与 SQLite 投影的 Plan/Spec 状态管理与约束 CLI

用法:
  plansql <command> [flags]

命令:
  list    列出所有已登记的 plan / spec 状态
  set     为指定的 plan / spec 设置状态并原子追加 SQL
  check   校验目标 SQL 文件的语法与有效性
  scan    扫描项目目录对齐 **/plans/** 和 **/specs/** 登记状态
  ui      在默认浏览器中打开可视化看板
  serve   启动后台守护进程 (Daemon 模式)
  mcp     以 stdio 模式启动 MCP 桥接服务
  schema  导出完整契约的 JSON Schema 目录

命令选项 (flags):
  --json        以结构化 JSON 输出
  --schema      零 I/O 导出当前命令对应的 JSON Schema 契约
  --host        指定 daemon 地址 (默认自动探测)
  --help, -h    显示帮助信息
  --version, -v 显示版本`)
}

func printFullSchema() {
	schemas := map[string]any{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"name":    "plansql",
		"version": "1.0.0",
		"tools": []map[string]any{
			{
				"name":         "plansql_list",
				"description":  "List all tracked plan and spec status entries from the SQLite projection.",
				"outputSchema": getListSchema(),
			},
			{
				"name":        "plansql_set_status",
				"description": "Update a plan/spec status by safely appending an atomic SQL mutation to the append-only SQL log.",
				"inputSchema": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"path":   map[string]any{"type": "string", "description": "Relative path to markdown document"},
						"status": map[string]any{"type": "string", "description": "Status name (completed, in_progress, pending, abandoned) or JSON"},
						"type":   map[string]any{"type": "string", "enum": []string{"plan", "spec"}},
					},
					"required": []string{"path", "status"},
				},
				"outputSchema": getSetSchema(),
			},
			{
				"name":         "plansql_check",
				"description":  "Verify the syntax and integrity of the append-only SQL file.",
				"outputSchema": getCheckSchema(),
			},
			{
				"name":         "plansql_scan",
				"description":  "Scan the workspace file system for **/plans/** and **/specs/** to reconcile with recorded SQL status.",
				"outputSchema": getScanSchema(),
			},
		},
	}
	PrintJSON(schemas)
}

func getScanSchema() map[string]any {
	return map[string]any{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"type":    "object",
		"properties": map[string]any{
			"ok": map[string]any{"type": "boolean"},
			"data": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"total_discovered": map[string]any{"type": "integer", "description": "磁盘上扫描到的规划文档总数"},
					"total_registered": map[string]any{"type": "integer", "description": "SQL 中已登记记录总数"},
					"aligned": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"type":          map[string]any{"type": "string", "enum": []string{"plan", "spec"}},
								"path":          map[string]any{"type": "string"},
								"registered":    map[string]any{"type": "boolean"},
								"status":        map[string]any{"type": "string"},
								"state_summary": map[string]any{"type": "string"},
							},
							"required": []string{"type", "path", "registered"},
						},
					},
					"unregistered": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"type":          map[string]any{"type": "string", "enum": []string{"plan", "spec"}},
								"path":          map[string]any{"type": "string"},
								"registered":    map[string]any{"type": "boolean"},
								"state_summary": map[string]any{"type": "string"},
							},
							"required": []string{"type", "path"},
						},
					},
					"dangling": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"id":         map[string]any{"type": "integer"},
								"type":       map[string]any{"type": "string"},
								"path":       map[string]any{"type": "string"},
								"status":     map[string]any{"type": "string"},
								"created_at": map[string]any{"type": "string"},
								"updated_at": map[string]any{"type": "string"},
							},
							"required": []string{"id", "type", "path", "status"},
						},
					},
				},
				"required": []string{"total_discovered", "total_registered", "aligned", "unregistered", "dangling"},
			},
		},
		"required": []string{"ok", "data"},
	}
}

func getListSchema() map[string]any {
	return map[string]any{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"type":    "object",
		"properties": map[string]any{
			"ok": map[string]any{"type": "boolean"},
			"data": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id":         map[string]any{"type": "integer"},
						"type":       map[string]any{"type": "string", "enum": []string{"plan", "spec"}},
						"path":       map[string]any{"type": "string"},
						"status":     map[string]any{"type": "string"},
						"created_at": map[string]any{"type": "string"},
						"updated_at": map[string]any{"type": "string"},
					},
					"required": []string{"id", "type", "path", "status"},
				},
			},
		},
		"required": []string{"ok", "data"},
	}
}

func getCheckSchema() map[string]any {
	return map[string]any{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"type":    "object",
		"properties": map[string]any{
			"ok": map[string]any{"type": "boolean"},
			"data": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"total_statements": map[string]any{"type": "integer"},
					"valid":            map[string]any{"type": "boolean"},
					"errors":           map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				},
				"required": []string{"total_statements", "valid", "errors"},
			},
		},
		"required": []string{"ok", "data"},
	}
}

func getSetSchema() map[string]any {
	return map[string]any{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"type":    "object",
		"properties": map[string]any{
			"ok": map[string]any{"type": "boolean"},
			"data": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"message": map[string]any{"type": "string"},
					"sql":     map[string]any{"type": "string"},
				},
				"required": []string{"message", "sql"},
			},
		},
		"required": []string{"ok", "data"},
	}
}

func runServe(args []string) int {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	addr := fs.String("addr", "127.0.0.1:18090", "监听地址")
	root := fs.String("root", "", "项目根目录 (留空自动向上探测 .git 或 plans_status.sql)")
	sqlFile := fs.String("sql", "", "SQL 文件路径 (留空默认在项目根目录下的 plans_status.sql)")
	autoExit := fs.Bool("auto-exit", false, "闲置 30 分钟后自动退出")
	_ = fs.Parse(args)

	// 智能确定项目根目录：优先命令行传入，否则向上递归查找包含 .git 或 plans_status.sql 的目录
	resolvedRoot := *root
	if resolvedRoot == "" {
		cwd, err := os.Getwd()
		if err != nil {
			cwd = "."
		}
		resolvedRoot = daemon.FindProjectRoot(cwd)
	}
	absRoot, _ := filepath.Abs(resolvedRoot)

	absSQL := *sqlFile
	if absSQL == "" {
		absSQL = filepath.Join(absRoot, "plans_status.sql")
	} else if !filepath.IsAbs(absSQL) {
		absSQL = filepath.Join(absRoot, absSQL)
	}

	cfg := daemon.Config{
		Addr:        *addr,
		RootDir:     absRoot,
		SQLFile:     absSQL,
		IdleTimeout: 30 * time.Minute,
		AutoExit:    *autoExit,
	}

	srv, err := daemon.NewServer(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[daemon] init server failed: %v\n", err)
		return 1
	}

	fmt.Printf("[daemon] 启动守护进程: %s\n", cfg.Addr)
	fmt.Printf("[daemon] 项目根目录: %s\n", cfg.RootDir)
	fmt.Printf("[daemon] SQL 状态文件: %s\n", cfg.SQLFile)

	if err := srv.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "[daemon] server error: %v\n", err)
		return 1
	}
	return 0
}

func runList(args []string) int {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	host := fs.String("host", "", "daemon 地址")
	jsonOut := fs.Bool("json", false, "以 JSON 输出")
	schemaOut := fs.Bool("schema", false, "导出 list 的 JSON Schema 契约")
	_ = fs.Parse(args)

	// 零 I/O 极速反射契约
	if *schemaOut {
		PrintJSON(getListSchema())
		return 0
	}

	client, err := NewClient(*host)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	if err := EnsureDaemonRunning(client, ".", "plans_status.sql"); err != nil {
		fmt.Fprintf(os.Stderr, "error ensuring daemon: %v\n", err)
		return 1
	}

	items, err := client.GetItems()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to get items: %v\n", err)
		return 1
	}

	if *jsonOut {
		PrintJSON(map[string]any{"ok": true, "data": items})
		return 0
	}

	if len(items) == 0 {
		fmt.Println("未发现任何登记的 plan 或 spec 记录。使用 `plansql scan` 查看未登记文档。")
		return 0
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "ID\tTYPE\tPATH\tSTATUS\tUPDATED_AT")
	for _, it := range items {
		// 紧凑展示状态
		compactStatus := strings.ReplaceAll(it.Status, "\n", "")
		if len(compactStatus) > 40 {
			compactStatus = compactStatus[:37] + "..."
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\n", it.ID, it.Type, it.Path, compactStatus, it.UpdatedAt)
	}
	w.Flush()
	return 0
}

func runCheck(args []string) int {
	fs := flag.NewFlagSet("check", flag.ExitOnError)
	host := fs.String("host", "", "daemon 地址")
	jsonOut := fs.Bool("json", false, "以 JSON 输出")
	schemaOut := fs.Bool("schema", false, "导出 check 的 JSON Schema 契约")
	_ = fs.Parse(args)

	// 零 I/O 极速反射契约
	if *schemaOut {
		PrintJSON(getCheckSchema())
		return 0
	}

	client, err := NewClient(*host)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	if err := EnsureDaemonRunning(client, ".", "plans_status.sql"); err != nil {
		fmt.Fprintf(os.Stderr, "error ensuring daemon: %v\n", err)
		return 1
	}

	res, err := client.CheckSQL()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to check SQL: %v\n", err)
		return 1
	}

	if *jsonOut {
		PrintJSON(map[string]any{"ok": res.Valid, "data": res})
		if !res.Valid {
			return 2
		}
		return 0
	}

	if res.Valid {
		fmt.Printf("✅ SQL 校验完全正常！共包含 %d 条有效语句。\n", res.TotalStatements)
		return 0
	}

	fmt.Fprintf(os.Stderr, "❌ 检测到 %d 处 SQL 语法或约束错误:\n", len(res.Errors))
	for _, e := range res.Errors {
		fmt.Fprintf(os.Stderr, "  - %s\n", e)
	}
	return 2
}

func runSet(args []string) int {
	fs := flag.NewFlagSet("set", flag.ExitOnError)
	statusFlag := fs.String("status", "completed", "状态简写 (pending, in_progress, completed) 或完整 JSON")
	typeFlag := fs.String("type", "", "文档类型 (plan 或 spec，留空自动根据路径推导)")
	host := fs.String("host", "", "daemon 地址")
	schemaOut := fs.Bool("schema", false, "导出 set 的 JSON Schema 契约")
	_ = fs.Parse(args)

	// 零 I/O 极速反射契约
	if *schemaOut {
		PrintJSON(getSetSchema())
		return 0
	}

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "用法: plansql set <path> [--status completed] [--type plan|spec]")
		return 1
	}

	targetPath := filepath.ToSlash(filepath.Clean(fs.Arg(0)))

	itemType := *typeFlag
	if itemType == "" {
		if strings.Contains("/"+targetPath+"/", "/specs/") {
			itemType = "spec"
		} else {
			itemType = "plan"
		}
	}

	statusVal := *statusFlag
	// 如果传入的是简写单词，转换为标准 JSON 结构
	if statusVal == "completed" || statusVal == "in_progress" || statusVal == "pending" || statusVal == "abandoned" {
		statusVal = fmt.Sprintf(`{"state":"%s"}`, statusVal)
	} else {
		// 校验是否是合法 JSON
		var js any
		if err := json.Unmarshal([]byte(statusVal), &js); err != nil {
			fmt.Fprintf(os.Stderr, "错误: --status 不是合法的 JSON 字符串: %v\n", err)
			return 1
		}
	}

	client, err := NewClient(*host)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	if err := EnsureDaemonRunning(client, ".", "plans_status.sql"); err != nil {
		fmt.Fprintf(os.Stderr, "error ensuring daemon: %v\n", err)
		return 1
	}

	sqlGenerated, err := client.AppendMutation(itemType, targetPath, statusVal)
	if err != nil {
		fmt.Fprintf(os.Stderr, "更新失败: %v\n", err)
		return 1
	}

	fmt.Printf("✅ 已成功追加并落库状态变更:\n%s\n", sqlGenerated)
	return 0
}

func runScan(args []string) int {
	fs := flag.NewFlagSet("scan", flag.ExitOnError)
	host := fs.String("host", "", "daemon 地址")
	jsonOut := fs.Bool("json", false, "以 JSON 输出")
	schemaOut := fs.Bool("schema", false, "导出 scan 的 JSON Schema 契约")
	_ = fs.Parse(args)

	// 零 I/O 极速反射契约：符合标准 v1/v2，必须零 I/O 极速反射
	if *schemaOut {
		PrintJSON(getScanSchema())
		return 0
	}

	client, err := NewClient(*host)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	if err := EnsureDaemonRunning(client, ".", "plans_status.sql"); err != nil {
		fmt.Fprintf(os.Stderr, "error ensuring daemon: %v\n", err)
		return 1
	}

	report, err := client.ScanDocs()
	if err != nil {
		fmt.Fprintf(os.Stderr, "扫描对齐失败: %v\n", err)
		return 1
	}

	if *jsonOut {
		PrintJSON(map[string]any{"ok": true, "data": report})
		return 0
	}

	fmt.Printf("🔍 项目文档扫描对齐报告:\n")
	fmt.Printf("  - 磁盘扫描到文档: %d 篇\n", report.TotalDiscovered)
	fmt.Printf("  - SQL 已登记文档: %d 篇\n", report.TotalRegistered)
	fmt.Printf("  - 正常已对齐:     %d 篇\n", len(report.Aligned))
	fmt.Printf("  - 未登记文档:     %d 篇\n", len(report.Unregistered))
	fmt.Printf("  - 悬空已失效记录: %d 条\n", len(report.Dangling))

	if len(report.Unregistered) > 0 {
		fmt.Println("\n[未登记的文档清单 (建议登记)]:")
		for _, u := range report.Unregistered {
			fmt.Printf("  * [%s] %s\n", u.Type, u.Path)
		}
	}

	if len(report.Dangling) > 0 {
		fmt.Println("\n[悬空记录 (磁盘文件已不存在)]:")
		for _, d := range report.Dangling {
			fmt.Printf("  ! [%s] %s\n", d.Type, d.Path)
		}
	}
	return 0
}

func runUI(args []string) int {
	fs := flag.NewFlagSet("ui", flag.ExitOnError)
	host := fs.String("host", "", "daemon 地址")
	_ = fs.Parse(args)

	client, err := NewClient(*host)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	if err := EnsureDaemonRunning(client, ".", "plans_status.sql"); err != nil {
		fmt.Fprintf(os.Stderr, "error ensuring daemon: %v\n", err)
		return 1
	}

	url := client.baseURL
	fmt.Printf("🌐 正在唤起默认浏览器打开看板: %s\n", url)
	openBrowser(url)
	return 0
}

func openBrowser(url string) {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", url}
	case "darwin":
		cmd = "open"
		args = []string{url}
	default:
		cmd = "xdg-open"
		args = []string{url}
	}
	_ = exec.Command(cmd, args...).Start()
}
