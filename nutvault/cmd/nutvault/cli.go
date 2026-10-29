//go:build cli

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"nutvault/internal/api"
	"nutvault/internal/app"
	"nutvault/internal/core"
	"nutvault/internal/storage"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		return
	}

	command := os.Args[1]
	args := os.Args[2:]

	switch command {
	case "add":
		handleAdd(args)
	case "list":
		handleList(args)
	case "backup":
		handleBackup(args)
	case "snapshots":
		handleSnapshots(args)
	case "diff":
		handleDiff(args)
	case "restore":
		handleRestore(args)
	case "serve":
		handleServe(args)
	case "config":
		handleConfig(args)
	case "version":
		handleVersion(args)
	default:
		fmt.Fprintf(os.Stderr, "未知子命令: %s\n\n", command)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Printf("NutVault CLI - 坚果云轻量文件版本备份命令行 (v%s, %s 通道)\n\n", app.Version, app.Channel())
	fmt.Println("用法:")
	fmt.Println("  nutvault-cli add <file-path> [--max-size MB] [--keep N] [--no-schedule]")
	fmt.Println("  nutvault-cli list [--json]")
	fmt.Println("  nutvault-cli backup [target-id | --all]")
	fmt.Println("  nutvault-cli snapshots <target-id> [--limit N]")
	fmt.Println("  nutvault-cli diff <snapshot-id>")
	fmt.Println("  nutvault-cli restore <snapshot-id> [--out path]")
	fmt.Println("  nutvault-cli serve [--port 8765]")
	fmt.Println("  nutvault-cli config [get|set] [key] [value]")
	fmt.Println("  nutvault-cli version")
}

func openStore() *storage.Store {
	store, err := storage.NewStore(app.DefaultDBPath())
	if err != nil {
		fmt.Fprintf(os.Stderr, "连接数据库失败: %v\n", err)
		os.Exit(1)
	}
	return store
}

func handleAdd(args []string) {
	fs := flag.NewFlagSet("add", flag.ExitOnError)
	maxSizeMB := fs.Float64("max-size", 0, "单文件大小上限 (MB)")
	keep := fs.Int("keep", 30, "保留历史版本快照数")
	noSchedule := fs.Bool("no-schedule", false, "关闭自动定时备份检查")

	_ = fs.Parse(args)
	rem := fs.Args()
	if len(rem) < 1 {
		fmt.Println("请指定要保护的文件绝对路径。")
		return
	}

	targetPath := rem[0]
	info := core.AnalyzePath(targetPath)
	if _, err := os.Stat(info.SourcePath); err != nil {
		fmt.Fprintf(os.Stderr, "本地文件不存在: %s\n", info.SourcePath)
		os.Exit(1)
	}

	store := openStore()
	defer store.Close()

	if existing, _ := store.GetTargetBySourcePath(info.SourcePath); existing != nil {
		fmt.Println("该文件保护规则已存在 (ID:", existing.ID, ")")
		return
	}

	target := &storage.Target{
		SourcePath:      info.SourcePath,
		SourceDir:       info.SourceDir,
		FileName:        info.FileName,
		MappedDir:       info.MappedDir,
		MaxSizeLimit:    int64(*maxSizeMB * 1024 * 1024),
		KeepVersions:    *keep,
		ScheduleEnabled: !*noSchedule,
	}

	saved, err := store.AddTarget(target)
	if err != nil {
		fmt.Fprintf(os.Stderr, "保存保护规则失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("成功添加保护规则 [ID: %d]!\n", saved.ID)
	fmt.Printf("  源文件: %s\n", saved.SourcePath)
	fmt.Printf("  坚果云映射目录: %s\n", saved.MappedDir)

	eng := core.NewEngine(store)
	res, err := eng.BackupTarget(saved, false)
	if err != nil {
		fmt.Printf("  首次备份提示: %v\n", err)
	} else if res != nil {
		fmt.Printf("  首次快照已生成: %s\n", res.SnapshotFilename)
	}
}

func handleList(args []string) {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "以 JSON 格式输出")
	_ = fs.Parse(args)

	store := openStore()
	defer store.Close()

	targets, err := store.ListTargets()
	if err != nil {
		fmt.Fprintf(os.Stderr, "获取目标列表失败: %v\n", err)
		os.Exit(1)
	}

	if *asJSON {
		_ = json.NewEncoder(os.Stdout).Encode(targets)
		return
	}

	if len(targets) == 0 {
		fmt.Println("当前无保护文件规则。使用 'nutvault-cli add <path>' 添加。")
		return
	}

	fmt.Printf("%-4s  %-24s  %-30s  %-20s\n", "ID", "文件名", "坚果云映射目录", "最近备份时间")
	fmt.Println(strings.Repeat("-", 85))
	for _, t := range targets {
		lastTime := "从未备份"
		if t.LastBackupAt != nil {
			lastTime = t.LastBackupAt.Format("2006-01-02 15:04:05")
		}
		fmt.Printf("%-4d  %-24s  %-30s  %-20s\n", t.ID, truncate(t.FileName, 24), truncate(t.MappedDir, 30), lastTime)
	}
}

func handleBackup(args []string) {
	store := openStore()
	defer store.Close()
	eng := core.NewEngine(store)

	if len(args) == 0 || args[0] == "--all" {
		sched := core.NewScheduler(eng, store)
		results := sched.TriggerAll()
		fmt.Printf("完成批量备份，共处理 %d 个规则项:\n", len(results))
		for _, r := range results {
			fmt.Printf("  [%d] %s -> %s (%s)\n", r.TargetID, filepath.Base(r.SourcePath), r.Status, r.Message)
		}
		return
	}

	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		fmt.Fprintf(os.Stderr, "无效目标 ID: %s\n", args[0])
		os.Exit(1)
	}

	target, err := store.GetTargetByID(id)
	if err != nil || target == nil {
		fmt.Fprintf(os.Stderr, "未找到目标规则 ID: %d\n", id)
		os.Exit(1)
	}

	res, err := eng.BackupTarget(target, true)
	if err != nil && res == nil {
		fmt.Fprintf(os.Stderr, "备份失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("备份完成: %s [%s]\n", res.SnapshotFilename, res.Status)
}

func handleSnapshots(args []string) {
	if len(args) < 1 {
		fmt.Println("请指定目标规则 ID: nutvault-cli snapshots <target-id>")
		return
	}
	targetID, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		fmt.Fprintf(os.Stderr, "无效目标 ID: %s\n", args[0])
		os.Exit(1)
	}

	store := openStore()
	defer store.Close()

	snaps, err := store.ListSnapshotsForTarget(targetID, 50)
	if err != nil {
		fmt.Fprintf(os.Stderr, "查询快照失败: %v\n", err)
		os.Exit(1)
	}

	if len(snaps) == 0 {
		fmt.Println("该规则项暂无历史快照。")
		return
	}

	fmt.Printf("%-6s  %-30s  %-10s  %-20s\n", "快照ID", "快照文件名", "大小", "备份时间")
	fmt.Println(strings.Repeat("-", 75))
	for _, s := range snaps {
		fmt.Printf("%-6d  %-30s  %-10s  %-20s\n",
			s.ID, truncate(s.SnapshotFilename, 30),
			formatBytes(s.FileSizeBytes),
			s.CreatedAt.Format("2006-01-02 15:04:05"))
	}
}

func handleDiff(args []string) {
	if len(args) < 1 {
		fmt.Println("请指定快照 ID: nutvault-cli diff <snapshot-id>")
		return
	}
	snapID, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		fmt.Fprintf(os.Stderr, "无效快照 ID: %s\n", args[0])
		os.Exit(1)
	}

	store := openStore()
	defer store.Close()
	eng := core.NewEngine(store)

	diff, err := eng.CompareSnapshotWithSource(snapID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "比对失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("比对摘要: %s\n", diff.Summary)
	fmt.Printf("源文件: %s (%s)\n", diff.SourcePath, formatBytes(diff.SourceSize))
	fmt.Printf("坚果云快照: %s (%s)\n\n", diff.SnapshotPath, formatBytes(diff.SnapshotSize))

	if diff.Identical {
		fmt.Println("✓ 快照与当前本地文件内容完全相同。")
		return
	}

	if diff.IsBinary {
		fmt.Println("二进制文件，无法输出逐行差异。")
		return
	}

	for _, line := range diff.Lines {
		switch line.Type {
		case "added":
			fmt.Printf("\033[32m+ %s\033[0m\n", line.Content)
		case "removed":
			fmt.Printf("\033[31m- %s\033[0m\n", line.Content)
		default:
			fmt.Printf("  %s\n", line.Content)
		}
	}
}

func handleRestore(args []string) {
	fs := flag.NewFlagSet("restore", flag.ExitOnError)
	outPath := fs.String("out", "", "指定恢复目标路径 (默认恢复到原始源文件)")
	_ = fs.Parse(args)
	rem := fs.Args()
	if len(rem) < 1 {
		fmt.Println("请指定快照 ID: nutvault-cli restore <snapshot-id> [--out path]")
		return
	}

	snapID, err := strconv.ParseInt(rem[0], 10, 64)
	if err != nil {
		fmt.Fprintf(os.Stderr, "无效快照 ID: %s\n", rem[0])
		os.Exit(1)
	}

	store := openStore()
	defer store.Close()
	eng := core.NewEngine(store)

	res, err := eng.RestoreSnapshot(snapID, *outPath, true)
	if err != nil {
		fmt.Fprintf(os.Stderr, "恢复快照失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ 恢复成功!\n")
	fmt.Printf("  目标路径: %s\n", res.SourcePath)
	fmt.Printf("  来源快照: %s\n", res.RestoredFrom)
	if res.PreRestorePath != "" {
		fmt.Printf("  已自动创建恢复前安全副本: %s\n", res.PreRestorePath)
	}
}

func handleServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	port := fs.Int("port", 8765, "HTTP 监听端口")
	_ = fs.Parse(args)

	store := openStore()
	defer store.Close()

	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *port))
	if err != nil {
		fmt.Fprintf(os.Stderr, "监听端口失败: %v\n", err)
		os.Exit(1)
	}

	server := api.NewServer(store, api.Meta{
		Channel:      app.Channel(),
		BuildMode:    app.BuildMode,
		Version:      app.Version,
		DBPath:       app.DefaultDBPath(),
		IsAutoStart:  app.IsAutoStartEnabled,
		SetAutoStart: app.SetAutoStart,
	})

	url := api.URL(listener)
	fmt.Printf("NutVault WebUI 正在运行: %s\n", url)
	fmt.Println("按 Ctrl+C 停止服务...")

	if err := server.Serve(listener); err != nil {
		fmt.Fprintf(os.Stderr, "服务异常终止: %v\n", err)
	}
}

func handleConfig(args []string) {
	store := openStore()
	defer store.Close()

	if len(args) == 0 {
		cfgs, _ := store.GetAllConfigs()
		fmt.Println("当前配置项:")
		for k, v := range cfgs {
			fmt.Printf("  %s = %s\n", k, v)
		}
		return
	}

	op := args[0]
	if op == "get" && len(args) >= 2 {
		val, _ := store.GetConfig(args[1], "")
		fmt.Println(val)
		return
	}
	if op == "set" && len(args) >= 3 {
		_ = store.SetConfig(args[1], args[2])
		fmt.Printf("已更新 %s = %s\n", args[1], args[2])
		return
	}
	fmt.Println("用法: nutvault-cli config [get <key> | set <key> <value>]")
}

func handleVersion(_ []string) {
	fmt.Printf("NutVault CLI version=%s mode=%s channel=%s\n", app.Version, app.BuildMode, app.Channel())
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

func formatBytes(bytes int64) string {
	if bytes <= 0 {
		return "0 B"
	}
	const k = 1024
	sizes := []string{"B", "KB", "MB", "GB"}
	var i int
	b := float64(bytes)
	for b >= k && i < len(sizes)-1 {
		b /= k
		i++
	}
	return fmt.Sprintf("%.2f %s", b, sizes[i])
}
