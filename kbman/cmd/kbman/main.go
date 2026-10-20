package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"kbman/internal/contract"
	"kbman/internal/service"
)

const (
	progName = "kbman"
	version  = "0.1.0"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	switch cmd {
	case "help", "-h", "--help":
		printUsage()
	case "version", "-v", "--version":
		fmt.Printf("%s %s\n", progName, version)
	case "schema":
		emit(contract.Catalog(progName, version))
	case "migrate":
		handleMigrate(os.Args[2:])
	case "check":
		handleCheck(os.Args[2:])
	case "report":
		handleReport(os.Args[2:])
	case "touch":
		handleTouch(os.Args[2:])
	default:
		fail("unknown_command", fmt.Sprintf("未知子命令: %s", cmd))
	}
}

func printUsage() {
	fmt.Print(`kbman - 知识库时序防腐与生命周期治理引擎

用法:
  kbman <command> [flags]
  kbman schema            导出离线 JSON 契约目录
  kbman --version         显示版本

命令:
  migrate    批量扫描旧笔记并安全注入标准时序元数据（只追加缺失字段）
  check      前置检查条目健康度，deprecated 时以退出码 2 阻止执行
  report     被报错证伪时原子更新状态并追加自愈排障日志
  touch      实操验证通过后刷新 last_verified

所有命令均支持 --json（标准包络）与 --schema（离线契约）。
`)
}

func getDefaultKBDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return filepath.Join(home, ".gemini", "config", "skills", "kb-engineering-notes")
}

func handleMigrate(args []string) {
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	dir := fs.String("dir", getDefaultKBDir(), "知识库目录路径")
	force := fs.Bool("force", false, "是否强制覆盖已有元数据")
	asJSON := fs.Bool("json", false, "以 JSON 包络输出")
	wantSchema := fs.Bool("schema", false, "只导出该命令的离线契约")
	if err := fs.Parse(args); err != nil {
		fail("invalid_args", err.Error())
	}

	if *wantSchema {
		emitCommandSchema("migrate")
		return
	}

	res, err := service.MigrateDir(*dir, *force)
	if err != nil {
		failJSON(*asJSON, "migrate_failed", fmt.Sprintf("迁移失败: %v", err))
	}

	if *asJSON {
		emit(envelopeData(res))
		return
	}

	fmt.Printf("✅ 扫描完成: 共扫描 %d 篇笔记，成功升级 %d 篇，跳过 %d 篇。\n", res.TotalFiles, res.UpdatedFiles, res.SkippedFiles)
	for _, e := range res.Errors {
		fmt.Printf("  - 警告: %s\n", e)
	}
}

func handleCheck(args []string) {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	dir := fs.String("dir", getDefaultKBDir(), "知识库目录路径")
	asJSON := fs.Bool("json", false, "以 JSON 包络输出")
	wantSchema := fs.Bool("schema", false, "只导出该命令的离线契约")
	if err := fs.Parse(args); err != nil {
		fail("invalid_args", err.Error())
	}

	if *wantSchema {
		emitCommandSchema("check")
		return
	}

	target, err := requireTarget(fs.Args())
	if err != nil {
		failJSON(*asJSON, "missing_target", err.Error())
	}

	filePath, err := service.FindEntry(*dir, target)
	if err != nil {
		failJSON(*asJSON, "entry_not_found", fmt.Sprintf("查找失败: %v", err))
	}

	report, err := service.CheckEntry(filePath)
	if err != nil {
		failJSON(*asJSON, "check_failed", fmt.Sprintf("检查失败: %v", err))
	}

	if *asJSON {
		emit(envelopeData(report))
	} else {
		fmt.Printf("条目: [%s] (文件: %s)\n", report.ID, filepath.Base(report.FilePath))
		fmt.Printf("状态: %s (最后实测: %s)\n", report.Status, report.LastVerified)
		if report.Warning != "" {
			fmt.Println(report.Warning)
		}
	}

	if !report.CanExecute {
		os.Exit(2)
	}
}

func handleReport(args []string) {
	fs := flag.NewFlagSet("report", flag.ContinueOnError)
	dir := fs.String("dir", getDefaultKBDir(), "知识库目录路径")
	status := fs.String("status", "suspect", "更新后的状态 (verified/suspect/deprecated)")
	reason := fs.String("reason", "", "变更原因说明")
	appendLog := fs.String("append", "", "在正文末尾追加的排障现场或报错日志")
	asJSON := fs.Bool("json", false, "以 JSON 包络输出")
	wantSchema := fs.Bool("schema", false, "只导出该命令的离线契约")
	if err := fs.Parse(args); err != nil {
		fail("invalid_args", err.Error())
	}

	if *wantSchema {
		emitCommandSchema("report")
		return
	}

	target, err := requireTarget(fs.Args())
	if err != nil {
		failJSON(*asJSON, "missing_target", err.Error())
	}

	filePath, err := service.FindEntry(*dir, target)
	if err != nil {
		failJSON(*asJSON, "entry_not_found", fmt.Sprintf("查找失败: %v", err))
	}

	if err := service.UpdateEntry(filePath, *status, *reason, *appendLog); err != nil {
		failJSON(*asJSON, "update_failed", fmt.Sprintf("更新失败: %v", err))
	}

	result := map[string]string{"entry": target, "action": "report", "status": *status}
	if *asJSON {
		emit(envelopeData(result))
		return
	}
	fmt.Printf("✅ 成功回写条目 [%s]: 状态更新为 [%s]，最后验证时间已刷新。\n", target, *status)
}

func handleTouch(args []string) {
	fs := flag.NewFlagSet("touch", flag.ContinueOnError)
	dir := fs.String("dir", getDefaultKBDir(), "知识库目录路径")
	asJSON := fs.Bool("json", false, "以 JSON 包络输出")
	wantSchema := fs.Bool("schema", false, "只导出该命令的离线契约")
	if err := fs.Parse(args); err != nil {
		fail("invalid_args", err.Error())
	}

	if *wantSchema {
		emitCommandSchema("touch")
		return
	}

	target, err := requireTarget(fs.Args())
	if err != nil {
		failJSON(*asJSON, "missing_target", err.Error())
	}

	filePath, err := service.FindEntry(*dir, target)
	if err != nil {
		failJSON(*asJSON, "entry_not_found", fmt.Sprintf("查找失败: %v", err))
	}

	if err := service.UpdateEntry(filePath, "", "", ""); err != nil {
		failJSON(*asJSON, "update_failed", fmt.Sprintf("刷新失败: %v", err))
	}

	result := map[string]string{"entry": target, "action": "touch"}
	if *asJSON {
		emit(envelopeData(result))
		return
	}
	fmt.Printf("✅ 条目 [%s] last_verified 验证时间已刷新至今天。\n", target)
}

func requireTarget(args []string) (string, error) {
	if len(args) < 1 {
		return "", fmt.Errorf("错误: 请指定条目 ID (如 002) 或完整文件名")
	}
	return args[0], nil
}

func envelopeData(data any) map[string]any {
	return map[string]any{"ok": true, "data": data}
}

func emit(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		fail("encode_failed", fmt.Sprintf("输出失败: %v", err))
	}
}

func emitCommandSchema(name string) {
	contractDoc, ok := contract.CommandContract(name)
	if !ok {
		fail("unknown_command", fmt.Sprintf("未知子命令: %s", name))
	}
	emit(contractDoc)
}

func failJSON(asJSON bool, code, message string) {
	if asJSON {
		emit(map[string]any{
			"ok":    false,
			"error": map[string]string{"code": code, "message": message},
		})
	} else {
		fmt.Fprintln(os.Stderr, message)
	}
	os.Exit(1)
}

func fail(code, message string) {
	fmt.Fprintf(os.Stderr, "%s\n", message)
	os.Exit(1)
}
