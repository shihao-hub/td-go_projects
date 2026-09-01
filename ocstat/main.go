package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const version = "0.1.0"

// 注意：可执行文件名必须用 ocstat.exe，不能以 stats.exe 结尾。
// 公司飞连（CorpLink）云端黑名单存在 *stats.exe 通配规则，
// 任何以 stats.exe 结尾的 exe 都会被判「侵权风险」拦截（2026-09 实测）。

type options struct {
	watch    bool
	interval time.Duration
	limit    int
	showAll  bool
	switched bool
	dbPath   string
}

func main() {
	var opt options

	args := os.Args[1:]
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		if args[0] == "watch" {
			opt.watch = true
		} else {
			fmt.Fprintf(os.Stderr, "未知子命令: %s（仅支持 watch）\n", args[0])
			os.Exit(2)
		}
		args = args[1:]
	}

	fs := flag.NewFlagSet("ocstat", flag.ExitOnError)
	fs.Usage = func() { usage(fs) }
	fs.DurationVar(&opt.interval, "i", 5*time.Second, "watch 模式刷新间隔")
	fs.IntVar(&opt.limit, "n", 30, "明细展示最近 N 条会话")
	fs.BoolVar(&opt.showAll, "all", false, "明细展示全部会话")
	fs.BoolVar(&opt.switched, "switched", false, "只展示切换过模型/档位的会话")
	fs.StringVar(&opt.dbPath, "db", defaultDBPath(), "opencode.db 路径")
	fs.Parse(args)

	if opt.interval <= 0 {
		fmt.Fprintln(os.Stderr, "-i 必须为正时长，例如 5s / 10s")
		os.Exit(2)
	}

	if opt.watch {
		if err := runWatch(opt); err != nil {
			fmt.Fprintln(os.Stderr, "错误:", err)
			os.Exit(1)
		}
		return
	}
	if err := runOnce(opt); err != nil {
		fmt.Fprintln(os.Stderr, "错误:", err)
		os.Exit(1)
	}
}

func runOnce(opt options) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	st, err := openStore(ctx, opt.dbPath)
	if err != nil {
		return err
	}
	defer st.Close()
	snap, err := st.snapshot(ctx)
	if err != nil {
		return err
	}
	render(os.Stdout, snap, opt)
	return nil
}

func runWatch(opt options) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	fmt.Print("\x1b[?25l")
	defer fmt.Print("\x1b[?25h")

	refresh := func() {
		start := time.Now()
		fmt.Print("\x1b[H\x1b[2J")
		rctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		st, err := openStore(rctx, opt.dbPath)
		if err != nil {
			fmt.Fprintf(os.Stdout, "错误: %v\n\n（保持监听，%s 后重试 · Ctrl+C 退出）\n", err, opt.interval)
			return
		}
		snap, err := st.snapshot(rctx)
		st.Close()
		if err != nil {
			fmt.Fprintf(os.Stdout, "错误: %v\n\n（保持监听，%s 后重试 · Ctrl+C 退出）\n", err, opt.interval)
			return
		}
		render(os.Stdout, snap, opt)
		fmt.Printf("\n每 %s 刷新 · 本轮耗时 %dms · Ctrl+C 退出\n", opt.interval, time.Since(start).Milliseconds())
	}

	refresh()
	t := time.NewTicker(opt.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			fmt.Println()
			return nil
		case <-t.C:
			refresh()
		}
	}
}

func defaultDBPath() string {
	if v := os.Getenv("OPENCODE_DATA"); v != "" {
		return filepath.Join(v, "opencode.db")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "opencode.db"
	}
	return filepath.Join(home, ".local", "share", "opencode", "opencode.db")
}

func usage(fs *flag.FlagSet) {
	fmt.Fprintf(os.Stderr, `ocstat %s — opencode 会话模型/思考档位统计

用法:
  ocstat [选项]          打印一次统计
  ocstat watch [选项]    常驻模式，定时清屏刷新

选项:
`, version)
	fs.PrintDefaults()
}
