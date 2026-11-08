//go:build cli

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"deepseeksearch/internal/api"
	"deepseeksearch/internal/app"
	"deepseeksearch/internal/deepseek"
	"deepseeksearch/internal/storage"
)

func main() {
	defer app.Recover(false)

	if len(os.Args) < 2 {
		printUsage()
		return
	}

	if os.Args[1] == "--hidden" || os.Args[1] == "--tray" {
		handleServe(nil)
		return
	}

	switch command := os.Args[1]; command {
	case "search":
		handleSearch(os.Args[2:])
	case "fetch":
		handleFetch(os.Args[2:])
	case "config":
		handleConfig(os.Args[2:])
	case "history":
		handleHistory(os.Args[2:])
	case "serve":
		handleServe(os.Args[2:])
	case "where":
		handleWhere(os.Args[2:])
	case "version", "-v", "--version":
		modeStr := "development"
		if app.IsRelease() {
			modeStr = "release"
		}
		fmt.Printf("DeepSeek Search Studio %s (channel: %s, build: %s)\n", app.Version, modeStr, app.BuildMode)
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
	fmt.Printf(`DeepSeek Search Studio - 深度搜索与网页萃取工作台

用法:
  %[1]s <command> [arguments...]

通道说明:
  deepseeksearch-cli.exe  Console 通道（就是本程序）：终端 / 脚本 / Agent 专用，
                          管道与输出重定向全合规。
  deepseeksearch-gui.exe  GUI 通道：只负责桌面工作台（窗口 + 托盘），不解析子命令。

命令:
  search     调用 DeepSeek Search API 进行精准网页搜索（支持 --json / --content / --count / --refresh）
  fetch      调用 DeepSeek Fetch API 抓取指定 URL 并提取为纯净 Markdown（支持 --save / --refresh）
  config     配置 API Key 与基础参数 (show / set-key / set-url / clear-key / test)
  history    查看最近搜索或提取历史
  serve      启动内置 WebUI 与 HTTP 服务 (支持 --port / --open)
  where      自省：查看当前通道、数据库路径与 GUI 运行状态
  version    打印版本与通道信息

使用 '%[1]s <command> --help' 获取更多选项。
`, prog)
}

func resolveDBPath(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	return app.DefaultDBPath()
}

func handleSearch(args []string) {
	fs := flag.NewFlagSet("search", flag.ExitOnError)
	count := fs.Int("count", 10, "返回结果条数 (1~50)")
	ori := fs.String("ori", "", "原始问题/意图上下文 (ori_question)")
	content := fs.Bool("content", false, "是否直接抓取正文 (need_content)")
	asJSON := fs.Bool("json", false, "以结构化 JSON 输出")
	refresh := fs.Bool("refresh", false, "强制重新向远端请求，忽略本地近期搜索缓存")
	key := fs.String("key", "", "临时覆盖 API Key")
	dbPath := fs.String("db", "", "自定义 SQLite 数据库路径")
	_ = fs.Parse(args)

	query := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if query == "" {
		fmt.Fprintln(os.Stderr, "错误: 请提供搜索关键词。例如: deepseeksearch-cli search \"DeepSeek 搜索接口\"")
		os.Exit(1)
	}

	store, err := storage.NewStore(resolveDBPath(*dbPath))
	if err != nil {
		fmt.Fprintf(os.Stderr, "打开存储库失败: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	// 1. 若非强制刷新，优先检查 24 小时内的本地搜索缓存（0 元消耗）
	if !*refresh {
		cached, cacheErr := store.GetCachedSearch(query, *count, *content, 24*time.Hour)
		if cacheErr != nil {
			// 缓存读取失败不拦截请求，但必须留痕——静默 miss 会直接打到付费 API
			fmt.Fprintf(os.Stderr, "警告: 读取本地搜索缓存失败，将直接请求远端: %v\n", cacheErr)
		} else if cached != nil && cached.ResultsJSON != "" {
			var items []deepseek.SearchResultItem
			if err := json.Unmarshal([]byte(cached.ResultsJSON), &items); err == nil && len(items) > 0 {
				resp := deepseek.SearchResponse{
					Results:   items,
					FromCache: true,
					CachedAt:  &cached.CreatedAt,
				}
				if *asJSON {
					enc := json.NewEncoder(os.Stdout)
					enc.SetIndent("", "  ")
					_ = enc.Encode(resp)
					return
				}
				fmt.Printf("📦 [本地缓存命中] (检索于 %s，消耗 0 元，加 --refresh 强制刷新远端)\n", cached.CreatedAt.Local().Format("2006-01-02 15:04:05"))
				fmt.Printf("🔍 检索到 %d 条结果 (Query: %q):\n\n", len(items), query)
				for i, r := range items {
					scoreStr := fmt.Sprintf("%.2f", r.RerankScore)
					fmt.Printf("[%d] %s\n", i+1, r.Title)
					fmt.Printf("    链接: %s\n", r.URL)
					fmt.Printf("    相关度: %s", scoreStr)
					if r.Authority != nil {
						fmt.Printf(" | 权威度: %v", r.Authority)
					}
					fmt.Println()
					if r.Snippet != "" {
						fmt.Printf("    片段: %s\n", r.Snippet)
					}
					if r.Summary != "" {
						fmt.Printf("    LLM摘要: %s\n", strings.ReplaceAll(r.Summary, "\n", " "))
					}
					fmt.Println()
				}
				return
			}
		}
	}

	cfg, cfgErr := store.GetConfig()
	if cfgErr != nil {
		fmt.Fprintf(os.Stderr, "读取配置失败: %v\n", cfgErr)
		os.Exit(1)
	}
	apiKey := cfg.APIKey
	if *key != "" {
		apiKey = *key
	}
	if strings.TrimSpace(apiKey) == "" {
		fmt.Fprintln(os.Stderr, "错误: 尚未配置 DeepSeek API Key。请先运行: deepseeksearch-cli config set-key <sk-...>")
		os.Exit(1)
	}

	client := deepseek.NewClient(apiKey, cfg.BaseURL)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	resp, err := client.Search(ctx, deepseek.SearchRequest{
		Query:       query,
		Count:       *count,
		OriQuestion: *ori,
		NeedContent: *content,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "搜索失败: %v\n", err)
		os.Exit(1)
	}

	// 记录到本地
	if _, histErr := store.AddSearchHistory(query, *count, *content, len(resp.Results), resp.Results); histErr != nil {
		fmt.Fprintf(os.Stderr, "警告: 记录搜索历史失败: %v\n", histErr)
	}

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(resp)
		return
	}

	fmt.Printf("🔍 检索到 %d 条结果 (Query: %q):\n\n", len(resp.Results), query)
	for i, r := range resp.Results {
		scoreStr := fmt.Sprintf("%.2f", r.RerankScore)
		fmt.Printf("[%d] %s\n", i+1, r.Title)
		fmt.Printf("    链接: %s\n", r.URL)
		fmt.Printf("    相关度: %s", scoreStr)
		if r.Authority != nil {
			fmt.Printf(" | 权威度: %v", r.Authority)
		}
		fmt.Println()
		if r.Snippet != "" {
			fmt.Printf("    片段: %s\n", r.Snippet)
		}
		if r.Summary != "" {
			fmt.Printf("    LLM摘要: %s\n", strings.ReplaceAll(r.Summary, "\n", " "))
		}
		fmt.Println()
	}
}

func handleFetch(args []string) {
	fs := flag.NewFlagSet("fetch", flag.ExitOnError)
	savePath := fs.String("save", "", "保存 Markdown 内容到文件路径")
	asJSON := fs.Bool("json", false, "以结构化 JSON 输出")
	refresh := fs.Bool("refresh", false, "强制向远端重新抓取，忽略本地近期缓存")
	key := fs.String("key", "", "临时覆盖 API Key")
	dbPath := fs.String("db", "", "自定义 SQLite 数据库路径")
	_ = fs.Parse(args)

	url := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if url == "" {
		fmt.Fprintln(os.Stderr, "错误: 请提供网页 URL。例如: deepseeksearch-cli fetch https://example.com")
		os.Exit(1)
	}

	store, err := storage.NewStore(resolveDBPath(*dbPath))
	if err != nil {
		fmt.Fprintf(os.Stderr, "打开存储库失败: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	// 1. 若非强制刷新，优先检查 7 天内的本地网页抓取缓存（0 元消耗）
	if !*refresh {
		cached, cacheErr := store.GetCachedFetch(url, 7*24*time.Hour)
		if cacheErr != nil {
			fmt.Fprintf(os.Stderr, "警告: 读取网页抓取缓存失败，将直接请求远端: %v\n", cacheErr)
		} else if cached != nil && cached.Content != "" {
			resp := deepseek.FetchResponse{
				Title:     cached.Title,
				Content:   cached.Content,
				FromCache: true,
				CachedAt:  &cached.FetchedAt,
			}
			if *savePath != "" {
				if err := os.WriteFile(*savePath, []byte(resp.Content), 0644); err != nil {
					fmt.Fprintf(os.Stderr, "保存文件失败: %v\n", err)
					os.Exit(1)
				}
				fmt.Printf("📦 [本地缓存命中] 成功提取并保存至: %s (标题: %s，消耗 0 元)\n", *savePath, resp.Title)
				return
			}
			if *asJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				_ = enc.Encode(resp)
				return
			}
			fmt.Printf("📦 [本地缓存命中] (抓取于 %s，消耗 0 元，加 --refresh 强制刷新远端)\n", cached.FetchedAt.Local().Format("2006-01-02 15:04:05"))
			fmt.Printf("# %s\n\n来源: %s\n\n", resp.Title, url)
			fmt.Println(resp.Content)
			return
		}
	}

	cfg, cfgErr := store.GetConfig()
	if cfgErr != nil {
		fmt.Fprintf(os.Stderr, "读取配置失败: %v\n", cfgErr)
		os.Exit(1)
	}
	apiKey := cfg.APIKey
	if *key != "" {
		apiKey = *key
	}
	if strings.TrimSpace(apiKey) == "" {
		fmt.Fprintln(os.Stderr, "错误: 尚未配置 DeepSeek API Key。请先运行: deepseeksearch-cli config set-key <sk-...>")
		os.Exit(1)
	}

	client := deepseek.NewClient(apiKey, cfg.BaseURL)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	resp, err := client.Fetch(ctx, deepseek.FetchRequest{URL: url})
	if err != nil {
		fmt.Fprintf(os.Stderr, "抓取网页失败: %v\n", err)
		os.Exit(1)
	}

	// 记录到本地
	if _, histErr := store.AddFetchHistory(url, resp.Title, resp.Content); histErr != nil {
		fmt.Fprintf(os.Stderr, "警告: 记录抓取历史失败: %v\n", histErr)
	}

	if *savePath != "" {
		if err := os.WriteFile(*savePath, []byte(resp.Content), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "保存文件失败: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("成功提取并保存至: %s (标题: %s)\n", *savePath, resp.Title)
		return
	}

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(resp)
		return
	}

	fmt.Printf("# %s\n\n来源: %s\n\n", resp.Title, url)
	fmt.Println(resp.Content)
}

func handleConfig(args []string) {
	if len(args) == 0 {
		fmt.Println("用法: deepseeksearch-cli config [show | set-key <key> | set-url <url> | clear-key | test]")
		return
	}

	store, err := storage.NewStore(resolveDBPath(""))
	if err != nil {
		fmt.Fprintf(os.Stderr, "打开存储库失败: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	cfg, cfgErr := store.GetConfig()
	if cfgErr != nil {
		fmt.Fprintf(os.Stderr, "读取配置失败: %v\n", cfgErr)
		os.Exit(1)
	}

	switch args[0] {
	case "show":
		maskedKey := "未配置"
		if len(cfg.APIKey) > 8 {
			maskedKey = cfg.APIKey[:4] + "****" + cfg.APIKey[len(cfg.APIKey)-4:]
		} else if cfg.APIKey != "" {
			maskedKey = "****"
		}
		fmt.Println("当前 DeepSeek Search Studio 配置:")
		fmt.Printf("  API Key:      %s\n", maskedKey)
		fmt.Printf("  Base URL:     %s\n", cfg.BaseURL)
		fmt.Printf("  默认条数:      %d\n", cfg.DefaultCount)
		fmt.Printf("  带正文默认:    %t\n", cfg.NeedContent)
		fmt.Printf("  数据存储目录:   %s\n", app.DataDir())

	case "set-key":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "错误: 请指定 API Key。例如: deepseeksearch-cli config set-key sk-xxxx")
			os.Exit(1)
		}
		cfg.APIKey = strings.TrimSpace(args[1])
		if err := store.SaveConfig(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "保存失败: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("API Key 已成功更新！")

	case "clear-key":
		cfg.APIKey = ""
		if err := store.SaveConfig(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "清空失败: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("API Key 已成功清空！")

	case "set-url":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "错误: 请指定 Base URL。例如: deepseeksearch-cli config set-url https://api.deepseek.com")
			os.Exit(1)
		}
		cfg.BaseURL = strings.TrimSpace(args[1])
		if err := store.SaveConfig(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "保存失败: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Base URL 已成功更新！")

	case "test":
		if strings.TrimSpace(cfg.APIKey) == "" {
			fmt.Fprintln(os.Stderr, "错误: 尚未配置 API Key")
			os.Exit(1)
		}
		fmt.Println("正在测试 DeepSeek 接口连通性（使用免费端点）...")
		client := deepseek.NewClient(cfg.APIKey, cfg.BaseURL)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := client.TestAuth(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "连通性测试未通过: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("连通性测试成功！接口响应正常（0 元消耗）。")
	default:
		fmt.Fprintf(os.Stderr, "未知子命令: %s\n", args[0])
	}
}

func handleHistory(args []string) {
	sub := "searches"
	if len(args) > 0 {
		sub = args[0]
	}

	store, err := storage.NewStore(resolveDBPath(""))
	if err != nil {
		fmt.Fprintf(os.Stderr, "打开存储库失败: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	if sub == "fetches" {
		fetches, err := store.ListFetchHistory(20)
		if err != nil {
			fmt.Fprintf(os.Stderr, "查询抓取历史失败: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("最近网页抓取历史 (%d 条):\n", len(fetches))
		for _, f := range fetches {
			fmt.Printf("- [%s] %s (%s)\n", f.FetchedAt.Local().Format("2006-01-02 15:04"), f.Title, f.URL)
		}
		return
	}

	searches, err := store.ListSearchHistory(20)
	if err != nil {
		fmt.Fprintf(os.Stderr, "查询搜索历史失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("最近搜索历史 (%d 条):\n", len(searches))
	for _, s := range searches {
		fmt.Printf("- [%s] %q (返回 %d 条)\n", s.CreatedAt.Local().Format("2006-01-02 15:04"), s.Query, s.ResultCount)
	}
}

func handleServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	port := fs.Int("port", 0, "HTTP 服务绑定端口 (0 表示动态寻找空闲端口)")
	openBrowser := fs.Bool("open", true, "启动后是否自动在默认浏览器中打开页面")
	dbPath := fs.String("db", "", "自定义 SQLite 数据库路径")
	_ = fs.Parse(args)

	realDBPath := resolveDBPath(*dbPath)
	store, err := storage.NewStore(realDBPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "打开数据库失败: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	server := api.NewServer(store, api.Meta{
		Channel:   app.Channel(),
		BuildMode: app.BuildMode,
		Version:   app.Version,
	})

	url, err := server.Start(*port, *openBrowser)
	if err != nil {
		fmt.Fprintf(os.Stderr, "启动 WebUI 失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("DeepSeek Search Studio WebUI 已就绪: %s\n", url)
	fmt.Println("按 Ctrl+C 终止服务...")

	// 阻塞等待
	select {}
}

func handleWhere(args []string) {
	dbPath := app.DefaultDBPath()
	guiAlive := app.GUIRunning(app.IsRelease())

	status := "未运行"
	if guiAlive {
		status = "正在运行 (活跃)"
	}

	fmt.Println("DeepSeek Search Studio 环境自省:")
	fmt.Printf("  运行通道:    %s (Console 命令行)\n", app.Channel())
	fmt.Printf("  构建形态:    %s\n", app.BuildMode)
	fmt.Printf("  版本号:      %s\n", app.Version)
	fmt.Printf("  数据根目录:  %s\n", app.DataDir())
	fmt.Printf("  默认数据库:  %s\n", dbPath)
	fmt.Printf("  GUI 进程:    %s\n", status)
}
