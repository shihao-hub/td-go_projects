// Package service 是 mail2feishu 的业务内核：
// 解析邮件 → 白名单过滤 → 防抖聚合 → 构建卡片 → 投递飞书。
package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"mail2feishu/internal/feishu"
	"mail2feishu/internal/matcher"
	"mail2feishu/internal/parser"
	"mail2feishu/internal/ratelimit"
)

const (
	sendTimeout  = 30 * time.Second
	summaryRunes = 800
)

// Options 是 Pipeline 的依赖与参数。
type Options struct {
	Matcher        *matcher.Matcher
	Feishu         *feishu.Client
	DebounceWindow time.Duration
	ThreadCooldown time.Duration
	Logger         *log.Logger
}

// Pipeline 串联白名单、防抖与飞书投递。
type Pipeline struct {
	opts      Options
	debouncer *ratelimit.Debouncer
}

// New 创建业务内核并启动防抖器。
func New(opts Options) (*Pipeline, error) {
	if opts.Matcher == nil {
		return nil, errors.New("matcher 不能为空")
	}
	if opts.Feishu == nil {
		return nil, errors.New("飞书客户端不能为空")
	}
	p := &Pipeline{opts: opts}
	p.debouncer = ratelimit.New(opts.DebounceWindow, opts.ThreadCooldown, p.send)
	return p, nil
}

// HandleMail 是 IMAP 监听器的回调入口。未命中白名单的邮件静默丢弃。
func (p *Pipeline) HandleMail(uid uint32, raw []byte) error {
	mail, err := parser.Parse(raw)
	if err != nil {
		return fmt.Errorf("解析邮件失败: %w", err)
	}
	if _, ok := p.opts.Matcher.Match(mail.From, mail.Subject); !ok {
		p.logf("UID %d 未命中白名单，已丢弃（from=%s subject=%q）", uid, mail.FromAddress, mail.Subject)
		return nil
	}
	key := threadKey(mail)
	if !p.debouncer.Push(ratelimit.Item{Key: key, Data: mail}) {
		p.logf("UID %d 处于冷却期，已丢弃（%s）", uid, key)
		return nil
	}
	return nil
}

// Close 停止防抖器并等待在途推送完成。
func (p *Pipeline) Close() {
	p.debouncer.Close()
}

func (p *Pipeline) send(item ratelimit.Item) {
	mail, ok := item.Data.(*parser.Mail)
	if !ok {
		p.logf("防抖事件数据类型异常: %T", item.Data)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	if err := p.opts.Feishu.SendCard(ctx, buildCardOptions(mail)); err != nil {
		p.logf("推送失败（%s）: %v", item.Key, err)
		return
	}
	p.logf("已推送（%s）", item.Key)
}

// threadKey 生成防抖与冷却的讨论串标识。
// GitHub 通知按仓库 + 编号聚合，其余邮件按 Message-ID，缺失时回退到发件人 + 主题。
func threadKey(mail *parser.Mail) string {
	if mail.GitHub != nil && mail.GitHub.Repo != "" && mail.GitHub.Number > 0 {
		return fmt.Sprintf("%s#%d", mail.GitHub.Repo, mail.GitHub.Number)
	}
	if mail.MessageID != "" {
		return mail.MessageID
	}
	return mail.FromAddress + "|" + mail.Subject
}

func buildCardOptions(mail *parser.Mail) feishu.CardOptions {
	opts := feishu.CardOptions{
		Title:    mail.Subject,
		Template: feishu.TemplateOrange,
		Sender:   mail.From,
		Subject:  mail.Subject,
		Summary:  parser.Summary(mail.Text, summaryRunes),
	}
	if opts.Title == "" {
		opts.Title = "新邮件提醒"
	}

	gh := mail.GitHub
	if gh == nil {
		return opts
	}

	opts.Title = fmt.Sprintf("%s · %s", gh.Repo, kindLabel(gh.Kind))
	if gh.Number > 0 {
		opts.Title = fmt.Sprintf("%s #%d", opts.Title, gh.Number)
	}
	opts.Template = feishu.TemplateBlue
	if gh.Kind == "pull" {
		opts.Template = feishu.TemplateGreen
	}
	opts.Meta = []feishu.MetaField{
		{Label: "仓库", Value: gh.Repo},
		{Label: "类型", Value: kindLabel(gh.Kind)},
		{Label: "触发人", Value: gh.Actor},
		{Label: "原因", Value: gh.Reason},
	}
	opts.LinkURL = gh.URL
	if gh.Number > 0 {
		opts.LinkText = fmt.Sprintf("打开 %s #%d", kindLabel(gh.Kind), gh.Number)
	} else {
		opts.LinkText = "在 GitHub 查看"
	}
	return opts
}

func kindLabel(kind string) string {
	if kind == "pull" {
		return "PR"
	}
	return "Issue"
}

func (p *Pipeline) logf(format string, args ...any) {
	if p.opts.Logger != nil {
		p.opts.Logger.Printf(format, args...)
	}
}
