// Package cli 是命令行的薄壳：解析参数 → 调 service → 渲染输出。
package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"

	"ccquota/internal/appenv"
	"ccquota/internal/model"
	"ccquota/internal/service"
)

// Options 是壳层共享的 I/O 与配置。
type Options struct {
	Stdout  io.Writer
	Stderr  io.Writer
	DBPath  string
	Version string
}

// envelope 是本仓约定的 JSON 包络。
type envelope struct {
	OK    bool   `json:"ok"`
	Data  any    `json:"data,omitempty"`
	Error *errObj `json:"error,omitempty"`
}

type errObj struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeJSON(w io.Writer, v any) {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func failJSON(w io.Writer, code, message string) {
	writeJSON(w, envelope{OK: false, Error: &errObj{Code: code, Message: message}})
}

// Run 分发子命令并返回退出码（0 成功 / 2 参数错误 / 1 其他失败）。
func Run(args []string, opt *Options) int {
	if len(args) == 0 {
		// 双击启动：直接拉起 WebUI 并打开浏览器（控制台随 web 常驻）。
		if launchedFromExplorer() {
			return runWeb([]string{"--open"}, opt)
		}
		fmt.Fprintln(opt.Stderr, usageText)
		return 2
	}
	switch args[0] {
	case "list":
		return runList(args[1:], opt)
	case "web":
		return runWeb(args[1:], opt)
	case "watch":
		return runWatch(args[1:], opt)
	case "doctor":
		return runDoctor(args[1:], opt)
	case "schema":
		return runSchema(args[1:], opt)
	case "url":
		return runURL(opt)
	case "-h", "--help", "help":
		fmt.Fprintln(opt.Stdout, usageText)
		return 0
	case "-v", "--version", "version":
		fmt.Fprintf(opt.Stdout, "ccquota %s\n", opt.Version)
		return 0
	default:
		fmt.Fprintf(opt.Stderr, "未知子命令: %s\n\n", args[0])
		fmt.Fprintln(opt.Stderr, usageText)
		return 2
	}
}

const usageText = `ccquota —— 只读渲染 cc-switch 已启用供应商的额度

用法：
  ccquota list    [--json] [--app claude|codex|kimi|agy] [--refresh]
  ccquota watch   [--interval 5m] [--once] [--app ...]
  ccquota web     [--addr 127.0.0.1:0] [--open]
  ccquota doctor  [--json]
  ccquota schema
  ccquota url

说明：
  list     列出额度卡片；默认读 5 分钟缓存，--refresh 强制刷新
  watch    终端常驻刷新（--once 只刷一次）
  web      启动本地 WebUI（随机端口，地址打印在 stdout）
  doctor   自检数据源、凭据与端点连通性
  schema   输出 JSON 契约（不访问数据库与网络）
  url      输出当前运行中的 WebUI 地址`

// runList 实现 list 子命令。
func runList(args []string, opt *Options) int {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.SetOutput(opt.Stderr)
	asJSON := fs.Bool("json", false, "输出 JSON 包络")
	app := fs.String("app", "", "只显示指定 app_type（claude/codex/kimi/agy）")
	refresh := fs.Bool("refresh", false, "忽略缓存，强制访问上游")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	svc, err := service.New(opt.DBPath)
	if err != nil {
		if *asJSON {
			failJSON(opt.Stdout, "init_failed", err.Error())
		} else {
			fmt.Fprintf(opt.Stderr, "ccquota: %v\n", err)
		}
		return 1
	}

	res, err := svc.Cards(context.Background(), service.Options{Refresh: *refresh, App: *app})
	if err != nil {
		if *asJSON {
			failJSON(opt.Stdout, "query_failed", err.Error())
		} else {
			fmt.Fprintf(opt.Stderr, "ccquota: %v\n", err)
		}
		return 1
	}

	if *asJSON {
		writeJSON(opt.Stdout, envelope{OK: true, Data: res})
		return 0
	}
	renderCards(opt.Stdout, res)
	return 0
}

// renderCards 输出人读卡片列表。
func renderCards(w io.Writer, res service.Result) {
	if len(res.Cards) == 0 {
		fmt.Fprintln(w, "没有找到已启用的额度查询供应商。")
		return
	}
	lastApp := ""
	for _, card := range res.Cards {
		if card.App != lastApp {
			if lastApp != "" {
				fmt.Fprintln(w)
			}
			fmt.Fprintf(w, "%s\n", appTitle(card.App))
			lastApp = card.App
		}
		title := card.Name
		if card.Current {
			title += "（使用中）"
		}
		fmt.Fprintf(w, "  %s\n", title)
		switch {
		case !card.OK && len(card.Tiers) == 0 && card.Balance == nil:
			fmt.Fprintf(w, "    ✗ %s\n", emptyAs(card.Err, "额度没查到"))
		case card.Kind == model.KindBalance:
			fmt.Fprintf(w, "    余额 %s\n", formatBalance(card.Balance))
			if card.Err != "" {
				fmt.Fprintf(w, "    ⚠ %s\n", card.Err)
			}
		default:
			parts := make([]string, 0, len(card.Tiers))
			for _, t := range card.Tiers {
				parts = append(parts, tierSummary(t))
			}
			fmt.Fprintf(w, "    %s\n", strings.Join(parts, "  |  "))
			if card.Err != "" {
				fmt.Fprintf(w, "    ⚠ %s（显示上次成功值）\n", card.Err)
			}
		}
	}
	if res.FromCache {
		fmt.Fprintf(w, "\n（缓存于 %s，--refresh 可强制刷新）\n", res.UpdatedAt.Local().Format("01-02 15:04:05"))
	}
}

func emptyAs(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

// runURL 输出运行中实例地址。
func runURL(opt *Options) int {
	rt, ok := appenv.DiscoverRuntime()
	if !ok {
		fmt.Fprintln(opt.Stderr, "当前没有运行中的 WebUI（先执行 ccquota web）")
		return 1
	}
	fmt.Fprintln(opt.Stdout, rt.URL)
	return 0
}
