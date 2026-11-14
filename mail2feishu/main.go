// mail2feishu 监听 QQ 邮箱新邮件，按白名单过滤后推送飞书群机器人。
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"mail2feishu/internal/buildinfo"
	"mail2feishu/internal/config"
	"mail2feishu/internal/feishu"
	imaplistener "mail2feishu/internal/imap"
	"mail2feishu/internal/matcher"
	"mail2feishu/internal/service"
)

func main() {
	os.Exit(run())
}

func run() int {
	configPath := flag.String("config", "", "配置文件路径（默认 <数据目录>/config.yaml）")
	showVersion := flag.Bool("version", false, "显示版本后退出")
	checkOnly := flag.Bool("check", false, "只校验配置并退出")
	testWebhook := flag.Bool("test-webhook", false, "发送一条测试卡片后退出")
	flag.Usage = func() {
		out := flag.CommandLine.Output()
		fmt.Fprintf(out, "mail2feishu %s\n\n", buildinfo.Version)
		fmt.Fprintln(out, "QQ 邮箱白名单邮件推送飞书群机器人。")
		fmt.Fprintln(out, "\n用法:")
		flag.PrintDefaults()
	}
	flag.Parse()

	if *showVersion {
		fmt.Println(versionString())
		return 0
	}

	dir, err := dataDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ %v\n", err)
		return 1
	}
	path := *configPath
	if path == "" {
		path = filepath.Join(dir, "config.yaml")
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

	cfg, err := config.Load(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ 配置无效: %v\n", err)
		return 1
	}

	if *testWebhook {
		if err := sendTestCard(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "❌ 测试卡片发送失败: %v\n", err)
			return 1
		}
		fmt.Println("✅ 测试卡片已发送，请检查飞书群")
		return 0
	}

	logger, closeLog, err := newLogger(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ 初始化日志失败: %v\n", err)
		return 1
	}
	defer closeLog()

	rules := cfg.Whitelist
	whitelist, err := matcher.New(rules)
	if err != nil {
		logger.Printf("白名单配置无效: %v", err)
		return 1
	}
	pipeline, err := service.New(service.Options{
		Matcher:        whitelist,
		Feishu:         feishu.NewClient(cfg.Feishu.WebhookURL, cfg.Feishu.Secret),
		DebounceWindow: cfg.Ratelimit.DebounceWindow.Std(),
		ThreadCooldown: cfg.Ratelimit.ThreadCooldown.Std(),
		Logger:         logger,
	})
	if err != nil {
		logger.Printf("初始化业务内核失败: %v", err)
		return 1
	}
	defer pipeline.Close()

	listener, err := imaplistener.New(imaplistener.Options{
		Host:         cfg.IMAP.Host,
		Port:         cfg.IMAP.Port,
		Username:     cfg.IMAP.Username,
		Password:     cfg.IMAP.Password,
		Mailbox:      cfg.IMAP.Mailbox,
		IdleTimeout:  cfg.IMAP.IdleTimeout.Std(),
		PollInterval: cfg.IMAP.PollInterval.Std(),
		OnMessage:    pipeline.HandleMail,
		Logger:       logger,
	})
	if err != nil {
		logger.Printf("初始化 IMAP 监听器失败: %v", err)
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger.Printf("mail2feishu %s 启动（模式 %s），配置 %s，白名单 %d 条，数据目录 %s",
		buildinfo.Version, buildinfo.Mode, path, len(rules), dir)

	if err := listener.Run(ctx); err != nil {
		logger.Printf("监听器异常退出: %v", err)
		return 1
	}
	logger.Printf("已停止")
	return 0
}

func versionString() string {
	return fmt.Sprintf("mail2feishu %s (%s, build %s)", buildinfo.Version, buildinfo.Mode, buildinfo.BuildID)
}

// dataDir 返回当前构建形态的数据目录，符合仓库数据隔离约定。
func dataDir() (string, error) {
	mode := "dev"
	if buildinfo.Mode == "release" {
		mode = "prod"
	}
	if base := os.Getenv("APPDATA"); base != "" {
		return filepath.Join(base, "language_projects", "mail2feishu", mode), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("无法定位用户目录: %w", err)
	}
	return filepath.Join(home, ".language_projects", "mail2feishu", mode), nil
}

func newLogger(dir string) (*log.Logger, func(), error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, nil, fmt.Errorf("创建数据目录失败: %w", err)
	}
	logPath := filepath.Join(dir, "mail2feishu.log")
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, nil, fmt.Errorf("打开日志文件失败: %w", err)
	}
	logger := log.New(io.MultiWriter(os.Stderr, file), "", log.LstdFlags)
	return logger, func() { _ = file.Close() }, nil
}

func sendTestCard(cfg *config.Config) error {
	client := feishu.NewClient(cfg.Feishu.WebhookURL, cfg.Feishu.Secret)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return client.SendCard(ctx, feishu.CardOptions{
		Title:    "mail2feishu 测试卡片",
		Template: feishu.TemplateGreen,
		Subject:  "配置验证成功",
		Summary:  "如果你看到这张卡片，说明飞书 Webhook 配置可用。",
	})
}
