package cli

import (
	"context"
	"flag"
	"fmt"
	"time"

	"ccquota/internal/service"
)

// runWatch 周期刷新并在终端重绘；--once 只刷新一次。
func runWatch(args []string, opt *Options) int {
	fs := flag.NewFlagSet("watch", flag.ContinueOnError)
	fs.SetOutput(opt.Stderr)
	interval := fs.Duration("interval", 5*time.Minute, "刷新间隔")
	once := fs.Bool("once", false, "只刷新一次后退出")
	app := fs.String("app", "", "只显示指定 app_type")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *interval < 10*time.Second {
		*interval = 10 * time.Second
	}

	svc, err := service.New(opt.DBPath)
	if err != nil {
		fmt.Fprintf(opt.Stderr, "ccquota: %v\n", err)
		return 1
	}

	for {
		res, err := svc.Cards(context.Background(), service.Options{Refresh: true, App: *app})
		if err != nil {
			fmt.Fprintf(opt.Stderr, "ccquota: %v\n", err)
			if *once {
				return 1
			}
		} else {
			// ANSI 清屏 + 光标归位；Windows Terminal 与 pwsh 7 均支持。
			fmt.Fprint(opt.Stdout, "\x1b[2J\x1b[H")
			fmt.Fprintf(opt.Stdout, "ccquota watch —— %s（每 %s 刷新，Ctrl+C 退出）\n\n",
				time.Now().Format("2006-01-02 15:04:05"), interval.String())
			renderCards(opt.Stdout, res)
		}
		if *once {
			return 0
		}
		time.Sleep(*interval)
	}
}
