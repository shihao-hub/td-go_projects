//go:build cli

// Console 通道入口：保留全部 CLI 语义，供终端、脚本与 Agent 使用。
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"mail2feishu/internal/app"
	"mail2feishu/internal/buildinfo"
	"mail2feishu/internal/config"
	"mail2feishu/internal/runtime"
)

func main() {
	os.Exit(run())
}

func run() int {
	configPath := flag.String("config", "", "配置文件路径（默认 <数据目录>/config.yaml）")
	showVersion := flag.Bool("version", false, "显示版本后退出")
	checkOnly := flag.Bool("check", false, "只校验配置并退出")
	testWebhook := flag.Bool("test-webhook", false, "发送一条测试卡片后退出")
	sendTestMail := flag.Bool("send-test", false, "用配置的 163 邮箱向 IMAP 账号发送测试邮件后退出")
	flag.Usage = func() {
		out := flag.CommandLine.Output()
		fmt.Fprintf(out, "mail2feishu %s\n\n", buildinfo.Version)
		fmt.Fprintln(out, "QQ 邮箱白名单邮件推送飞书群机器人（Console 通道）。")
		fmt.Fprintln(out, "无参数运行时前台常驻监听；GUI 请使用 mail2feishu-gui。")
		fmt.Fprintln(out, "\n用法:")
		flag.PrintDefaults()
	}
	flag.Parse()

	if *showVersion {
		fmt.Println(versionString())
		return 0
	}

	dataDir, err := app.DataDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ %v\n", err)
		return 1
	}
	path := *configPath
	if path == "" {
		path = filepath.Join(dataDir, "config.yaml")
	}

	if *checkOnly {
		cfg, err := config.Load(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ 配置无效: %v\n", err)
			return 1
		}
		fmt.Printf("✅ 配置有效: %s\n", path)
		fmt.Printf("   IMAP: %s:%d（%s）\n", cfg.IMAP.Host, cfg.IMAP.Port, cfg.IMAP.Username)
		fmt.Printf("   白名单规则: %d 条\n", len(cfg.Whitelist))
		return 0
	}

	logger, closeLog, err := app.NewLogger(dataDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ 初始化日志失败: %v\n", err)
		return 1
	}
	defer closeLog()

	svc, err := runtime.New(runtime.Options{
		ConfigPath: path,
		DataDir:    dataDir,
		Logger:     logger,
	})
	if err != nil {
		logger.Printf("初始化失败: %v", err)
		return 1
	}
	defer svc.Close()

	if *testWebhook {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := svc.TestWebhook(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "❌ 测试卡片发送失败: %v\n", err)
			return 1
		}
		fmt.Println("✅ 测试卡片已发送，请检查飞书群")
		return 0
	}

	if *sendTestMail {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := svc.SendTestMail(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "❌ 测试邮件发送失败: %v\n", err)
			return 1
		}
		fmt.Printf("✅ 测试邮件已发送至 %s，请检查收件箱\n", svc.Config().IMAP.Username)
		return 0
	}

	releaseInstance, err := app.AcquireInstance()
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ %v\n", err)
		return 1
	}
	defer releaseInstance()

	if err := svc.Start(); err != nil {
		logger.Printf("启动失败: %v", err)
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger.Printf("mail2feishu %s 启动（模式 %s），配置 %s，数据目录 %s",
		buildinfo.Version, buildinfo.Mode, path, dataDir)
	<-ctx.Done()
	svc.Stop()
	logger.Printf("已停止")
	return 0
}

func versionString() string {
	return fmt.Sprintf("mail2feishu %s (%s, build %s)", buildinfo.Version, buildinfo.Mode, buildinfo.BuildID)
}
