//go:build cli

// Console 通道入口（anatomymanager-cli）：终端 / 脚本 / Agent 专用。
// 该产物为 console 子系统，父 shell 会正常等待进程退出，
// prompt、光标、管道与输出重定向全部符合预期。
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
	"anatomymanager/internal/app"
	"anatomymanager/internal/model"
	"anatomymanager/internal/storage"
)

func main() {
	defer app.Recover(false)

	if len(os.Args) < 2 {
		printUsage()
		return
	}

	// --hidden / --tray 在 console 通道退化为无窗口常驻服务（没有桌面壳可拉）。
	if os.Args[1] == "--hidden" || os.Args[1] == "--tray" {
		handleServe(nil)
		return
	}

	switch command := os.Args[1]; command {
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
	case "where":
		handleWhere(os.Args[2:])
	case "version", "-v", "--version":
		modeStr := "development"
		if app.IsRelease() {
			modeStr = "release"
		}
		fmt.Printf("Anatomy Manager %s (mode: %s, build: %s)\n", app.Version, modeStr, app.BuildMode)
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "未知子命令: %s\n", command)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	prog := strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe")
	fmt.Printf(`Anatomy Manager - 机械精绘与交互图谱管理器

用法:
  %[1]s [command] [flags]

通道说明:
  anatomymanager-cli.exe  Console 通道（就是本程序）：终端 / 脚本 / Agent 专用，
                          shell 正常等待，管道与输出重定向可用
  anatomymanager-gui.exe  GUI 通道：只负责桌面工作台，不接受任何子命令，
                          双击或带任意参数启动都是显示窗口（--hidden 为静默托盘）

命令:
  serve      启动内置 WebUI 与 HTTP 渲染服务 (默认自动探测可用端口, 支持 --port / --open)
  ingest     摄取并归档 anatomy 产物进入 SQLite 单文件数据库
  list       列出已收录的所有图谱 (支持 --json)
  get        查看指定图谱的元数据详情与文件清单 (支持 --json)
  schema     输出 Agent 协同 JSON Schema 规范 (ingest / artwork)
  export     将数据库中的指定作品导出为独立的本地目录
  where      自省：本二进制的通道、库路径、图谱数与各通道 GUI 运行状态 (支持 --json)
  version    打印版本信息

使用 '%[1]s <command> --help' 获取更多选项。`, prog)
}

// resolveDBPath 未显式指定 --db 时落到按构建通道隔离的规范数据目录。
func resolveDBPath(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	return app.DefaultDBPath()
}

// currentMeta 构造 WebUI 与自省命令共用的通道元数据。
func currentMeta(dbPath string) api.Meta {
	return api.Meta{
		Channel:   app.Channel(),
		BuildMode: app.BuildMode,
		Version:   app.Version,
		DBPath:    dbPath,
	}
}

// channelExeHint 返回与通道对应的 CLI 产物名（prod 无后缀，dev 带 -dev）。
func channelExeHint(channel string) string {
	if channel == "dev" {
		return "anatomymanager-cli-dev.exe"
	}
	return "anatomymanager-cli.exe"
}

func handleIngest(args []string) {
	fs := flag.NewFlagSet("ingest", flag.ExitOnError)
	name := fs.String("name", "", "作品唯一标识符 (如 rate-limiter)")
	title := fs.String("title", "", "人类可读标题")
	dir := fs.String("dir", "", "生成物目录路径")
	docPath := fs.String("doc", "", "原理解释 Markdown 文件路径")
	sessionID := fs.String("session-id", "", "来源 Agent 会话 ID")
	description := fs.String("description", "", "图谱简介（缺省时按来源会话自动生成）")
	category := fs.String("category", "figure", "图谱分类 (figure / part / exploded / rig)")
	tagsStr := fs.String("tags", "", "标签列表 (逗号分隔)")
	dbPath := fs.String("db", "", "SQLite 数据库路径 (默认 %APPDATA%\\language_projects\\anatomymanager\\<dev|prod>\\anatomy.db)")
	asJSON := fs.Bool("json", false, "以结构化 JSON 格式输出结果")

	_ = fs.Parse(args)

	if *name == "" {
		fatal("错误: 必须指定 --name", *asJSON)
	}
	if *dir == "" {
		fatal("错误: 必须指定 --dir 目录路径", *asJSON)
	}

	store, err := storage.NewStore(resolveDBPath(*dbPath))
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

	// 无会话 ID 时不能拼出"由 Agent 会话  生成"这种双空格病句，回落通用说明。
	displayDescription := *description
	if displayDescription == "" {
		if *sessionID != "" {
			displayDescription = fmt.Sprintf("由 Agent 会话 %s 生成的 anatomy 图谱", *sessionID)
		} else {
			displayDescription = "由 anatomy 生成物归档的机械原理图谱"
		}
	}

	artwork := &model.Artwork{
		ID:          *name,
		Title:       displayTitle,
		Category:    *category,
		Description: displayDescription,
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
			"channel":     app.Channel(),
			"dbPath":      resolveDBPath(*dbPath),
		}
		out, _ := json.MarshalIndent(res, "", "  ")
		fmt.Println(string(out))
	} else {
		fmt.Printf("✅ 成功摄取图谱 [%s] (%s)，已归档 %d 个文件到 SQLite。\n", artwork.ID, artwork.Title, len(fileContents))
		fmt.Printf("   通道: %s   数据库: %s\n", app.Channel(), resolveDBPath(*dbPath))
	}
}

func handleList(args []string) {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	dbPath := fs.String("db", "", "SQLite 数据库路径")
	asJSON := fs.Bool("json", false, "JSON 输出")
	_ = fs.Parse(args)

	store, err := storage.NewStore(resolveDBPath(*dbPath))
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
		fmt.Printf("暂无图谱。使用 'anatomymanager ingest' 导入。\n")
		fmt.Printf("通道: %s   数据库: %s\n", app.Channel(), resolveDBPath(*dbPath))
		return
	}

	fmt.Printf("%-18s %-25s %-10s %-12s\n", "ID", "标题", "分类", "更新时间")
	fmt.Println(strings.Repeat("-", 70))
	for _, a := range list {
		fmt.Printf("%-18s %-25s %-10s %-12s\n", a.ID, truncate(a.Title, 24), a.Category, a.UpdatedAt.Format("2006-01-02"))
	}
	fmt.Printf("\n通道: %s   数据库: %s\n", app.Channel(), resolveDBPath(*dbPath))
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

	store, err := storage.NewStore(resolveDBPath(*dbPath))
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

// whereOutput 是 `where` 子命令的机器可读契约，供 Agent 在执行 ingest 前自证通道与库位。
type whereOutput struct {
	Binary         string    `json:"binary"`
	Channel        string    `json:"channel"`
	BuildMode      string    `json:"buildMode"`
	Version        string    `json:"version"`
	DBPath         string    `json:"dbPath"`
	DBExists       bool      `json:"dbExists"`
	Artworks       int       `json:"artworks"`
	Files          int       `json:"files"`
	RunningGUIs    whereGUIs `json:"runningGuis"`
	Recommendation string    `json:"recommendation"`
}

type whereGUIs struct {
	Dev  bool `json:"dev"`
	Prod bool `json:"prod"`
}

// whereRecommendation 给出本二进制与运行中 GUI 的匹配结论。
// 口径：GUI 在跑就以它为目标库（否则用户刷新窗口看不到）；都没跑时默认 prod。
func whereRecommendation(channel string, guis whereGUIs) string {
	running := map[string]bool{"dev": guis.Dev, "prod": guis.Prod}
	if running[channel] {
		return "OK：本 CLI 与运行中的 GUI 同通道，写入后窗口点刷新即可见"
	}
	for _, other := range []string{"dev", "prod"} {
		if other != channel && running[other] {
			return fmt.Sprintf("注意：GUI 正运行在 %s 通道，要让窗口直接看到请改用 %s（或告知用户当前在看哪个通道）", other, channelExeHint(other))
		}
	}
	if channel == "prod" {
		return "当前无 GUI 运行；本 CLI 写入 prod 通道库，与用户日常双击的正式版一致（推荐默认）"
	}
	return "当前无 GUI 运行；本 CLI 写入 dev 通道库（仅供开发调试；非开发场景请改用 anatomymanager-cli.exe 写 prod）"
}

func handleWhere(args []string) {
	fs := flag.NewFlagSet("where", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "以结构化 JSON 输出")
	dbPath := fs.String("db", "", "覆盖数据库路径（仅调试用，Agent 不应传）")
	_ = fs.Parse(args)

	resolved := resolveDBPath(*dbPath)

	// 先 stat 再打开：where 是只读自省，不能因为好奇就在另一个通道里凭空造出空库。
	dbExists := false
	artworks, files := 0, 0
	if _, err := os.Stat(resolved); err == nil {
		dbExists = true
		if store, err := storage.NewStore(resolved); err == nil {
			artworks, files, _ = store.Stats()
			_ = store.Close()
		}
	}

	info := whereOutput{
		Binary:      filepath.Base(os.Args[0]),
		Channel:     app.Channel(),
		BuildMode:   app.BuildMode,
		Version:     app.Version,
		DBPath:      resolved,
		DBExists:    dbExists,
		Artworks:    artworks,
		Files:       files,
		RunningGUIs: whereGUIs{Dev: app.GUIRunning(false), Prod: app.GUIRunning(true)},
	}
	info.Recommendation = whereRecommendation(info.Channel, info.RunningGUIs)

	if *asJSON {
		out, _ := json.MarshalIndent(info, "", "  ")
		fmt.Println(string(out))
		return
	}

	guiState := func(running bool) string {
		if running {
			return "运行中"
		}
		return "未运行"
	}
	fmt.Printf("二进制    : %s\n", info.Binary)
	fmt.Printf("通道      : %s (%s)\n", info.Channel, info.BuildMode)
	fmt.Printf("版本      : %s\n", info.Version)
	if info.DBExists {
		fmt.Printf("数据库    : %s\n", info.DBPath)
		fmt.Printf("图库内容  : %d 个图谱 / %d 个文件\n", info.Artworks, info.Files)
	} else {
		fmt.Printf("数据库    : %s（尚未创建）\n", info.DBPath)
	}
	fmt.Printf("GUI 状态  : dev=%s  prod=%s\n", guiState(info.RunningGUIs.Dev), guiState(info.RunningGUIs.Prod))
	fmt.Printf("建议      : %s\n", info.Recommendation)
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
				"name":        map[string]any{"type": "string", "description": "唯一作品标识符，如 rate-limiter"},
				"title":       map[string]any{"type": "string", "description": "人类可读标题"},
				"dir":         map[string]any{"type": "string", "description": "生成的静态网页与脚本所在目录"},
				"doc":         map[string]any{"type": "string", "description": "原理解释 Markdown 文件路径"},
				"sessionId":   map[string]any{"type": "string", "description": "产生该作品的 Agent 会话 ID"},
				"description": map[string]any{"type": "string", "description": "图谱简介，缺省时按来源会话自动生成"},
				"category":    map[string]any{"type": "string", "enum": []string{"figure", "part", "exploded", "rig"}},
				"tags":        map[string]any{"type": "string", "description": "逗号分隔的标签"},
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

	store, err := storage.NewStore(resolveDBPath(*dbPath))
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
	port := fs.Int("port", 0, "HTTP 端口（0 = 由系统动态探测可用端口）")
	openBrowser := fs.Bool("open", false, "启动后自动打开系统默认浏览器")
	dbPath := fs.String("db", "", "SQLite 数据库路径")
	_ = fs.Parse(args)

	store, err := storage.NewStore(resolveDBPath(*dbPath))
	if err != nil {
		fmt.Fprintf(os.Stderr, "打开数据库失败: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	server := api.NewServer(store, currentMeta(resolveDBPath(*dbPath)))
	if _, err := server.Start(*port, *openBrowser); err != nil {
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
