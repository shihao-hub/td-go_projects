package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"anatomymanager/internal/api"
	"anatomymanager/internal/model"
	"anatomymanager/internal/storage"
)

const version = "1.0.0"

func main() {
	// 如果双击直接启动（无子命令传参），默认启动 WebUI 并自动弹出浏览器
	if len(os.Args) < 2 {
		handleServe([]string{"--open"})
		return
	}

	command := os.Args[1]
	switch command {
	case "ingest":
		handleIngest(os.Args[2:])
	case "list":
		handleList(os.Args[2:])
	case "get":
		handleGet(os.Args[2:])
	case "schema":
		handleSchema(os.Args[2:])
	case "export":
		handleExport(os.Args[2:])
	case "serve":
		handleServe(os.Args[2:])
	case "version", "-v", "--version":
		fmt.Printf("Anatomy Manager v%s (Go)\n", version)
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "未知子命令: %s\n", command)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`Anatomy Manager - 机械精绘与交互图谱管理器

用法:
  anatomymanager [command] [flags]
  (直接双击或不带参数运行：默认启动 WebUI 并打开浏览器)

命令:
  serve      启动内置 WebUI 与 HTTP 渲染服务 (默认端口 8765, 支持 --open)
  ingest     摄取并归档 anatomy 产物进入 SQLite 单文件数据库
  list       列出已收录的所有图谱 (支持 --json)
  get        查看指定图谱的元数据详情与文件清单 (支持 --json)
  schema     输出 Agent 协同 JSON Schema 规范 (ingest / artwork)
  export     将数据库中的指定作品导出为独立的本地目录
  version    打印版本信息

使用 'anatomymanager <command> --help' 获取更多选项。`)
}

func handleIngest(args []string) {
	fs := flag.NewFlagSet("ingest", flag.ExitOnError)
	name := fs.String("name", "", "作品唯一标识符 (如 rate-limiter)")
	title := fs.String("title", "", "人类可读标题")
	dir := fs.String("dir", "", "生成物目录路径")
	docPath := fs.String("doc", "", "原理解释 Markdown 文件路径")
	sessionID := fs.String("session-id", "", "来源 Agent 会话 ID")
	category := fs.String("category", "figure", "图谱分类 (figure / part / exploded / rig)")
	tagsStr := fs.String("tags", "", "标签列表 (逗号分隔)")
	dbPath := fs.String("db", "", "SQLite 数据库路径 (默认 ~/.anatomy/anatomy.db)")
	asJSON := fs.Bool("json", false, "以结构化 JSON 格式输出结果")

	_ = fs.Parse(args)

	if *name == "" {
		fatal("错误: 必须指定 --name", *asJSON)
	}
	if *dir == "" {
		fatal("错误: 必须指定 --dir 目录路径", *asJSON)
	}

	store, err := storage.NewStore(*dbPath)
	if err != nil {
		fatal(fmt.Sprintf("打开数据库失败: %v", err), *asJSON)
	}
	defer store.Close()

	// Scan directory files
	entries, err := os.ReadDir(*dir)
	if err != nil {
		fatal(fmt.Sprintf("读取目录失败: %v", err), *asJSON)
	}

	fileContents := make(map[string][]byte)
	primaryHTML := ""

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		fname := e.Name()
		// Only collect relevant files
		if isRelevantFile(fname) {
			fullPath := filepath.Join(*dir, fname)
			data, err := os.ReadFile(fullPath)
			if err != nil {
				continue
			}
			fileContents[fname] = data
			if strings.HasSuffix(fname, ".html") && !strings.Contains(fname, "light") && primaryHTML == "" {
				primaryHTML = fname
			}
		}
	}

	if primaryHTML == "" {
		// Fallback to any html
		for f := range fileContents {
			if strings.HasSuffix(f, ".html") {
				primaryHTML = f
				break
			}
		}
	}

	// Doc markdown
	docMarkdown := ""
	if *docPath != "" {
		data, err := os.ReadFile(*docPath)
		if err == nil {
			docMarkdown = string(data)
			// Also store doc as artifact file
			fileContents[filepath.Base(*docPath)] = data
		}
	}

	// Tags
	var tags []string
	if *tagsStr != "" {
		for _, t := range strings.Split(*tagsStr, ",") {
			t = strings.TrimSpace(t)
			if t != "" {
				tags = append(tags, t)
			}
		}
	}

	displayTitle := *title
	if displayTitle == "" {
		displayTitle = *name
	}

	artwork := &model.Artwork{
		ID:          *name,
		Title:       displayTitle,
		Category:    *category,
		Description: fmt.Sprintf("由 Agent 会话 %s 生成的 anatomy 图谱", *sessionID),
		Tags:        tags,
		SessionID:   *sessionID,
		PrimaryHTML: primaryHTML,
		DocMarkdown: docMarkdown,
		UpdatedAt:   time.Now(),
	}

	if err := store.SaveArtwork(artwork, fileContents); err != nil {
		fatal(fmt.Sprintf("入库失败: %v", err), *asJSON)
	}

	if *asJSON {
		res := map[string]any{
			"status":      "success",
			"artworkId":   artwork.ID,
			"title":       artwork.Title,
			"filesCount":  len(fileContents),
			"primaryHtml": primaryHTML,
			"savedAt":     artwork.UpdatedAt,
		}
		out, _ := json.MarshalIndent(res, "", "  ")
		fmt.Println(string(out))
	} else {
		fmt.Printf("✅ 成功摄取图谱 [%s] (%s)，已归档 %d 个文件到 SQLite。\n", artwork.ID, artwork.Title, len(fileContents))
	}
}

func handleList(args []string) {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	dbPath := fs.String("db", "", "SQLite 数据库路径")
	asJSON := fs.Bool("json", false, "JSON 输出")
	_ = fs.Parse(args)

	store, err := storage.NewStore(*dbPath)
	if err != nil {
		fatal(fmt.Sprintf("打开数据库失败: %v", err), *asJSON)
	}
	defer store.Close()

	list, err := store.ListArtworks()
	if err != nil {
		fatal(fmt.Sprintf("查询失败: %v", err), *asJSON)
	}

	if *asJSON {
		out, _ := json.MarshalIndent(list, "", "  ")
		fmt.Println(string(out))
		return
	}

	if len(list) == 0 {
		fmt.Println("暂无图谱。使用 'anatomymanager ingest' 导入。")
		return
	}

	fmt.Printf("%-18s %-25s %-10s %-12s\n", "ID", "标题", "分类", "更新时间")
	fmt.Println(strings.Repeat("-", 70))
	for _, a := range list {
		fmt.Printf("%-18s %-25s %-10s %-12s\n", a.ID, truncate(a.Title, 24), a.Category, a.UpdatedAt.Format("2006-01-02"))
	}
}

func handleGet(args []string) {
	fs := flag.NewFlagSet("get", flag.ExitOnError)
	dbPath := fs.String("db", "", "SQLite 数据库路径")
	asJSON := fs.Bool("json", false, "JSON 输出")
	_ = fs.Parse(args)

	if len(fs.Args()) < 1 {
		fatal("错误: 必须指定图谱 ID", *asJSON)
	}
	id := fs.Args()[0]

	store, err := storage.NewStore(*dbPath)
	if err != nil {
		fatal(fmt.Sprintf("打开数据库失败: %v", err), *asJSON)
	}
	defer store.Close()

	art, err := store.GetArtwork(id)
	if err != nil {
		fatal(fmt.Sprintf("未找到图谱 [%s]: %v", id, err), *asJSON)
	}

	if *asJSON {
		out, _ := json.MarshalIndent(art, "", "  ")
		fmt.Println(string(out))
		return
	}

	fmt.Printf("图谱 ID:    %s\n", art.ID)
	fmt.Printf("标题:       %s\n", art.Title)
	fmt.Printf("分类:       %s\n", art.Category)
	fmt.Printf("主渲染入口: %s\n", art.PrimaryHTML)
	fmt.Printf("来源会话:   %s\n", art.SessionID)
	fmt.Printf("归档文件清单:\n")
	for _, f := range art.Files {
		fmt.Printf("  - %s\n", f)
	}
}

func handleSchema(args []string) {
	topic := "ingest"
	if len(args) > 0 {
		topic = args[0]
	}

	switch topic {
	case "ingest":
		schema := map[string]any{
			"$schema": "https://json-schema.org/draft/2020-12/schema",
			"title":   "AnatomyIngestCommand",
			"type":    "object",
			"properties": map[string]any{
				"name":      map[string]any{"type": "string", "description": "唯一作品标识符，如 rate-limiter"},
				"title":     map[string]any{"type": "string", "description": "人类可读标题"},
				"dir":       map[string]any{"type": "string", "description": "生成的静态网页与脚本所在目录"},
				"doc":       map[string]any{"type": "string", "description": "原理解释 Markdown 文件路径"},
				"sessionId": map[string]any{"type": "string", "description": "产生该作品的 Agent 会话 ID"},
				"category":  map[string]any{"type": "string", "enum": []string{"figure", "part", "exploded", "rig"}},
				"tags":      map[string]any{"type": "string", "description": "逗号分隔的标签"},
			},
			"required": []string{"name", "dir"},
		}
		out, _ := json.MarshalIndent(schema, "", "  ")
		fmt.Println(string(out))
	case "artwork":
		schema := map[string]any{
			"$schema": "https://json-schema.org/draft/2020-12/schema",
			"title":   "ArtworkEntity",
			"type":    "object",
			"properties": map[string]any{
				"id":          map[string]any{"type": "string"},
				"title":       map[string]any{"type": "string"},
				"category":    map[string]any{"type": "string"},
				"description": map[string]any{"type": "string"},
				"tags":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"sessionId":   map[string]any{"type": "string"},
				"primaryHtml": map[string]any{"type": "string"},
				"files":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			},
		}
		out, _ := json.MarshalIndent(schema, "", "  ")
		fmt.Println(string(out))
	default:
		fmt.Fprintf(os.Stderr, "未知 schema 类型: %s (支持: ingest, artwork)\n", topic)
	}
}

func handleExport(args []string) {
	fs := flag.NewFlagSet("export", flag.ExitOnError)
	outDir := fs.String("out", "", "导出目标目录")
	dbPath := fs.String("db", "", "SQLite 数据库路径")
	_ = fs.Parse(args)

	if len(fs.Args()) < 1 {
		fmt.Fprintln(os.Stderr, "错误: 必须指定图谱 ID")
		os.Exit(1)
	}
	id := fs.Args()[0]

	if *outDir == "" {
		*outDir = filepath.Join(".", id)
	}

	store, err := storage.NewStore(*dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "打开数据库失败: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	art, err := store.GetArtwork(id)
	if err != nil {
		fmt.Fprintf(os.Stderr, "未找到图谱: %v\n", err)
		os.Exit(1)
	}

	_ = os.MkdirAll(*outDir, 0755)
	for _, fname := range art.Files {
		f, err := store.GetFile(art.ID, fname)
		if err != nil {
			continue
		}
		dest := filepath.Join(*outDir, fname)
		_ = os.WriteFile(dest, f.ContentBlob, 0644)
	}
	fmt.Printf("✅ 成功将图谱 [%s] 导出至: %s (%d 个文件)\n", art.ID, *outDir, len(art.Files))
}

func handleServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	port := fs.Int("port", 8765, "HTTP 端口")
	openBrowser := fs.Bool("open", false, "启动后自动打开系统默认浏览器")
	dbPath := fs.String("db", "", "SQLite 数据库路径")
	_ = fs.Parse(args)

	store, err := storage.NewStore(*dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "打开数据库失败: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	server := api.NewServer(store, *port)
	if err := server.Start(*openBrowser); err != nil {
		fmt.Fprintf(os.Stderr, "服务异常终止: %v\n", err)
		os.Exit(1)
	}
}

func isRelevantFile(fname string) bool {
	ext := strings.ToLower(filepath.Ext(fname))
	switch ext {
	case ".html", ".htm", ".png", ".jpg", ".svg", ".mjs", ".js", ".css", ".md", ".json":
		return true
	default:
		return false
	}
}

func fatal(msg string, asJSON bool) {
	if asJSON {
		res := map[string]any{"status": "error", "message": msg}
		out, _ := json.Marshal(res)
		fmt.Println(string(out))
	} else {
		fmt.Fprintln(os.Stderr, msg)
	}
	os.Exit(1)
}

func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) > max {
		return string(r[:max-3]) + "..."
	}
	return s
}
