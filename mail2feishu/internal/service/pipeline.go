// Package service 是 mail2feishu 的业务内核：
// 解析邮件 → 白名单过滤 → 持久化待投递 → 防抖聚合 → 构建卡片 → 投递飞书。
//
// 可靠性约定：HandleMail 返回 nil 表示这封邮件已经「落盘或有意丢弃」，
// 调用方（IMAP 监听器）可以推进 UID 水位线。邮件一旦写入 outbox，
// 即使进程退出、飞书投递失败或解析逻辑升级，重启后仍会继续投递。
package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	"mail2feishu/internal/audit"
	"mail2feishu/internal/feishu"
	"mail2feishu/internal/matcher"
	"mail2feishu/internal/outbox"
	"mail2feishu/internal/parser"
)

const (
	sendTimeout  = 30 * time.Second
	summaryRunes = 800

	defaultMaxAttempts = 5
	defaultRetryBase   = 30 * time.Second
	maxRetryDelay      = 10 * time.Minute
	// 终态记录保留 7 天：冷却判断依赖 sent 记录，保留期必须大于冷却窗口。
	terminalRetention = 7 * 24 * time.Hour
	// 投递租约在发送超时之外留出余量，避免慢请求被误判为中断。
	leaseBonus = 30 * time.Second

	minScanInterval = 20 * time.Millisecond
	maxScanInterval = 5 * time.Second
)

// Auditor 接收结构化审计事件。
type Auditor interface {
	Append(event audit.Event) error
}

// Options 是 Pipeline 的依赖与参数。
type Options struct {
	Matcher        *matcher.Matcher
	Feishu         *feishu.Client
	Outbox         *outbox.Store
	DebounceWindow time.Duration
	ThreadCooldown time.Duration
	// MaxAttempts 是单封邮件的最大投递次数（含首次），<=0 时使用默认 5 次。
	MaxAttempts int
	// RetryBase 是失败重试的基础退避，<=0 时使用默认 30s。
	RetryBase time.Duration
	Auditor   Auditor
	// OnPush 在每次投递结束后调用，供运行时更新界面状态。
	OnPush func(ok bool, err error)
	Logger *log.Logger
}

// Pipeline 串联白名单、持久化队列、防抖聚合与飞书投递。
type Pipeline struct {
	opts Options

	wake      chan struct{}
	cancel    context.CancelFunc
	done      chan struct{}
	closeOnce sync.Once
}

// New 创建业务内核并启动后台投递协程。
func New(opts Options) (*Pipeline, error) {
	if opts.Matcher == nil {
		return nil, errors.New("matcher 不能为空")
	}
	if opts.Feishu == nil {
		return nil, errors.New("飞书客户端不能为空")
	}
	if opts.Outbox == nil {
		return nil, errors.New("待投递队列不能为空")
	}
	if opts.DebounceWindow <= 0 {
		opts.DebounceWindow = time.Second
	}
	if opts.ThreadCooldown < 0 {
		opts.ThreadCooldown = 0
	}
	if opts.MaxAttempts <= 0 {
		opts.MaxAttempts = defaultMaxAttempts
	}
	if opts.RetryBase <= 0 {
		opts.RetryBase = defaultRetryBase
	}

	ctx, cancel := context.WithCancel(context.Background())
	p := &Pipeline{
		opts:   opts,
		wake:   make(chan struct{}, 1),
		cancel: cancel,
		done:   make(chan struct{}),
	}
	go p.loop(ctx)
	return p, nil
}

// HandleMail 是 IMAP 监听器的回调入口。
//
// 返回 nil 表示邮件已被持久化到 outbox 或被有意丢弃（未命中白名单 /
// 解析失败），监听器可以推进水位线；返回错误表示落盘失败，监听器必须
// 保留水位线，等下一轮重连重试。
func (p *Pipeline) HandleMail(uid, uidValidity uint32, raw []byte) error {
	mail, err := parser.Parse(raw)
	if err != nil {
		p.audit(audit.Event{
			Type:   audit.TypeMail,
			UID:    uid,
			Action: audit.ActionParseFailed,
			Error:  err.Error(),
		})
		p.logf("UID %d 解析失败（确定性错误，不再重试）: %v", uid, err)
		return nil
	}

	key := threadKey(mail)
	result, matched := p.opts.Matcher.Match(mail.From, mail.Subject)
	if !matched {
		p.audit(audit.Event{
			Type:    audit.TypeMail,
			UID:     uid,
			From:    mail.FromAddress,
			Subject: mail.Subject,
			Action:  audit.ActionDropped,
			Reason:  "未命中白名单",
			Thread:  key,
		})
		p.logf("UID %d 未命中白名单，已丢弃（from=%s subject=%q）", uid, mail.FromAddress, mail.Subject)
		return nil
	}

	p.audit(audit.Event{
		Type:    audit.TypeMail,
		UID:     uid,
		From:    mail.FromAddress,
		Subject: mail.Subject,
		Action:  audit.ActionWhitelisted,
		Rule:    result.Rule,
		Thread:  key,
	})

	now := time.Now()
	// 同一讨论串共享一个防抖窗口：以最早一封的截止时间为准，
	// 后续邮件只更新待推送内容，不延长窗口（与旧防抖语义一致）。
	readyAt := now.Add(p.opts.DebounceWindow)
	for _, existing := range p.opts.Outbox.List() {
		if existing.Thread != key || existing.State != outbox.StatePending {
			continue
		}
		if existing.ReadyAt.Before(readyAt) {
			readyAt = existing.ReadyAt
		}
	}
	entry := outbox.Entry{
		ID:          outbox.EntryID(uidValidity, uid),
		UID:         uid,
		UIDValidity: uidValidity,
		Thread:      key,
		From:        mail.FromAddress,
		Subject:     mail.Subject,
		Raw:         raw,
		State:       outbox.StatePending,
		ReadyAt:     readyAt,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := p.opts.Outbox.Put(entry); err != nil {
		return fmt.Errorf("写入待投递队列失败: %w", err)
	}
	p.wakeup()
	return nil
}

// Close 停止调度协程并等待在途投递结束；未投递的邮件保留在磁盘上。
func (p *Pipeline) Close() {
	p.closeOnce.Do(func() {
		p.cancel()
		<-p.done
	})
}

func (p *Pipeline) loop(ctx context.Context) {
	defer close(p.done)
	timer := time.NewTimer(0)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-p.wake:
			stopTimer(timer)
		case <-timer.C:
		}

		delay := p.process()
		if ctx.Err() != nil {
			return
		}
		timer.Reset(delay)
	}
}

func stopTimer(timer *time.Timer) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
}

func (p *Pipeline) wakeup() {
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

// process 执行一轮调度：恢复中断记录 → 投递到期邮件 → 清理终态记录。
// 返回距离下一次需要唤醒的时间。
func (p *Pipeline) process() time.Duration {
	now := time.Now()
	if recovered := p.opts.Outbox.RecoverExpiredLeases(now, sendTimeout+leaseBonus); recovered > 0 {
		p.logf("恢复 %d 条中断的待投递记录", recovered)
	}
	p.clampReadyAt(now)
	for _, entry := range p.dueEntries(now) {
		p.deliver(entry)
	}
	if removed := p.opts.Outbox.GC(now, terminalRetention); removed > 0 {
		p.logf("清理 %d 条过期投递记录", removed)
	}
	return p.nextDelay()
}

// clampReadyAt 把重启前遗留的、超出当前防抖窗口的待投递时间收敛到窗口内。
// 典型场景：退出时 pending 条目的 ReadyAt 是旧进程算出来的未来时间，
// 若用户在重启后调小了防抖窗口，不应继续按旧窗口干等。
func (p *Pipeline) clampReadyAt(now time.Time) {
	limit := now.Add(p.opts.DebounceWindow)
	for _, entry := range p.opts.Outbox.List() {
		if entry.State != outbox.StatePending || !entry.ReadyAt.After(limit) {
			continue
		}
		entry.ReadyAt = limit
		entry.UpdatedAt = now
		if err := p.opts.Outbox.Update(entry); err != nil {
			p.logf("收敛待投递时间失败（%s）: %v", entry.ID, err)
		}
	}
}

// dueEntries 返回当前可投递的邮件，同一讨论串只保留最新一封。
func (p *Pipeline) dueEntries(now time.Time) []outbox.Entry {
	byThread := make(map[string]outbox.Entry)
	var superseded []outbox.Entry
	for _, entry := range p.opts.Outbox.List() {
		if entry.State != outbox.StatePending || entry.ReadyAt.After(now) {
			continue
		}
		current, ok := byThread[entry.Thread]
		if !ok {
			byThread[entry.Thread] = entry
			continue
		}
		if isLater(entry, current) {
			superseded = append(superseded, current)
			byThread[entry.Thread] = entry
		} else {
			superseded = append(superseded, entry)
		}
	}
	// 窗口内同一讨论串只推送最新一封，其余按「已聚合」丢弃。
	for _, entry := range superseded {
		if err := p.opts.Outbox.Delete(entry.ID); err != nil {
			p.logf("清理聚合记录失败: %v", err)
		}
	}

	result := make([]outbox.Entry, 0, len(byThread))
	for _, entry := range byThread {
		result = append(result, entry)
	}
	sort.Slice(result, func(i, j int) bool {
		if !result[i].ReadyAt.Equal(result[j].ReadyAt) {
			return result[i].ReadyAt.Before(result[j].ReadyAt)
		}
		return result[i].UID < result[j].UID
	})
	return result
}

func isLater(a, b outbox.Entry) bool {
	if !a.ReadyAt.Equal(b.ReadyAt) {
		return a.ReadyAt.After(b.ReadyAt)
	}
	return a.UID > b.UID
}

func (p *Pipeline) deliver(entry outbox.Entry) {
	now := time.Now()
	if last := p.opts.Outbox.LastSent(entry.Thread); !last.IsZero() && now.Sub(last) < p.opts.ThreadCooldown {
		p.drop(entry, "冷却期丢弃", audit.ActionCooldownDropped)
		return
	}

	mail, err := parser.Parse(entry.Raw)
	if err != nil {
		p.logf("UID %d 重新解析失败: %v", entry.UID, err)
		p.fail(entry, fmt.Errorf("解析邮件失败: %w", err), now)
		return
	}

	entry.State = outbox.StateSending
	entry.LeaseUntil = now.Add(sendTimeout + leaseBonus)
	entry.UpdatedAt = now
	if err := p.opts.Outbox.Update(entry); err != nil {
		p.logf("标记投递中失败（%s）: %v", entry.ID, err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	start := time.Now()
	sendErr := p.opts.Feishu.SendCard(ctx, buildCardOptions(mail))
	elapsed := time.Since(start).Milliseconds()

	succeeded := sendErr == nil
	event := audit.Event{
		Type:       audit.TypePush,
		UID:        entry.UID,
		Thread:     entry.Thread,
		Action:     audit.ActionPushed,
		OK:         &succeeded,
		DurationMS: elapsed,
	}
	if sendErr != nil {
		event.Action = audit.ActionPushFailed
		event.Error = sendErr.Error()
	}
	p.audit(event)
	if p.opts.OnPush != nil {
		p.opts.OnPush(succeeded, sendErr)
	}

	now = time.Now()
	if sendErr == nil {
		entry.State = outbox.StateSent
		entry.Raw = nil
		entry.LeaseUntil = time.Time{}
		entry.LastError = ""
		entry.UpdatedAt = now
		if err := p.opts.Outbox.Update(entry); err != nil {
			p.logf("标记投递成功失败（%s）: %v", entry.ID, err)
		}
		p.logf("已推送（%s，%dms）", entry.Thread, elapsed)
		return
	}

	p.logf("推送失败（%s，第 %d/%d 次）: %v", entry.Thread, entry.Attempts+1, p.opts.MaxAttempts, sendErr)
	p.fail(entry, sendErr, now)
}

// fail 记录一次投递失败：未达上限时按指数退避重排，达上限转为 failed。
func (p *Pipeline) fail(entry outbox.Entry, sendErr error, now time.Time) {
	entry.Attempts++
	entry.LastError = sendErr.Error()
	entry.LeaseUntil = time.Time{}
	entry.UpdatedAt = now
	if entry.Attempts >= p.opts.MaxAttempts {
		entry.State = outbox.StateFailed
		p.logf("待投递 %s 重试 %d 次仍失败，停止自动重试: %v", entry.ID, entry.Attempts, sendErr)
	} else {
		entry.State = outbox.StatePending
		entry.ReadyAt = now.Add(p.retryDelay(entry.Attempts))
	}
	if err := p.opts.Outbox.Update(entry); err != nil {
		p.logf("更新待投递状态失败（%s）: %v", entry.ID, err)
	}
}

// drop 把邮件标记为有意丢弃并留痕：冷却期丢弃必须可从审计里区分出来。
func (p *Pipeline) drop(entry outbox.Entry, reason, action string) {
	p.audit(audit.Event{
		Type:    audit.TypeMail,
		UID:     entry.UID,
		From:    entry.From,
		Subject: entry.Subject,
		Action:  action,
		Reason:  reason,
		Thread:  entry.Thread,
	})
	entry.State = outbox.StateDropped
	entry.Raw = nil
	entry.LastError = reason
	entry.LeaseUntil = time.Time{}
	entry.UpdatedAt = time.Now()
	if err := p.opts.Outbox.Update(entry); err != nil {
		p.logf("标记丢弃失败（%s）: %v", entry.ID, err)
	}
	p.logf("UID %d %s（%s）", entry.UID, reason, entry.Thread)
}

func (p *Pipeline) retryDelay(attempts int) time.Duration {
	delay := p.opts.RetryBase
	for i := 1; i < attempts; i++ {
		delay *= 2
		if delay >= maxRetryDelay {
			return maxRetryDelay
		}
	}
	if delay > maxRetryDelay {
		return maxRetryDelay
	}
	return delay
}

// nextDelay 计算下一次调度时间：取最近一条 pending/sending 的时间点。
func (p *Pipeline) nextDelay() time.Duration {
	now := time.Now()
	var earliest time.Time
	for _, entry := range p.opts.Outbox.List() {
		var at time.Time
		switch entry.State {
		case outbox.StatePending:
			at = entry.ReadyAt
		case outbox.StateSending:
			at = entry.LeaseUntil
		default:
			continue
		}
		if earliest.IsZero() || at.Before(earliest) {
			earliest = at
		}
	}
	if earliest.IsZero() {
		return maxScanInterval
	}
	delay := earliest.Sub(now)
	if delay < minScanInterval {
		delay = minScanInterval
	}
	if delay > maxScanInterval {
		delay = maxScanInterval
	}
	return delay
}

func (p *Pipeline) audit(event audit.Event) {
	if p.opts.Auditor == nil {
		return
	}
	if err := p.opts.Auditor.Append(event); err != nil {
		p.logf("写审计日志失败: %v", err)
	}
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
