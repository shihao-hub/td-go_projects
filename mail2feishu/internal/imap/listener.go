// Package imap 实现 QQ 邮箱 IMAP IDLE 常驻监听：
// SSL 连接、新邮件事件、断线指数退避重连与轮询兜底。
package imap

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"strconv"
	"time"

	imapv1 "github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
)

const (
	initialBackoff = time.Second
	maxBackoff     = 2 * time.Minute
	commandTimeout = 2 * time.Minute
	maxMailBytes   = 10 << 20 // 单封邮件读取上限 10 MiB
)

// Options 是监听器的连接与回调配置。
type Options struct {
	Host         string
	Port         int
	Username     string
	Password     string
	Mailbox      string
	IdleTimeout  time.Duration
	PollInterval time.Duration
	// OnMessage 在收到新邮件时调用，raw 为完整 RFC 5322 原文。
	// 返回错误只记录日志，不会中断监听。
	OnMessage func(uid uint32, raw []byte) error
	Logger    *log.Logger
}

// Listener 是 IMAP IDLE 监听器。
type Listener struct {
	opts   Options
	logger *log.Logger

	// lastUID 是已处理邮件的 UID 水位线。它必须在重连之间保持，
	// 否则每次重连都会把断线期间到达的邮件当成历史邮件跳过。
	lastUID     uint32
	baselineSet bool
}

// New 校验并补全默认值。
func New(opts Options) (*Listener, error) {
	if opts.Username == "" {
		return nil, errors.New("IMAP 用户名不能为空")
	}
	if opts.Password == "" {
		return nil, errors.New("IMAP 密码不能为空")
	}
	if opts.OnMessage == nil {
		return nil, errors.New("OnMessage 回调不能为空")
	}
	if opts.Host == "" {
		opts.Host = "imap.qq.com"
	}
	if opts.Port == 0 {
		opts.Port = 993
	}
	if opts.Port < 1 || opts.Port > 65535 {
		return nil, fmt.Errorf("IMAP 端口非法: %d", opts.Port)
	}
	if opts.Mailbox == "" {
		opts.Mailbox = "INBOX"
	}
	if opts.IdleTimeout <= 0 {
		opts.IdleTimeout = 25 * time.Minute
	}
	if opts.PollInterval <= 0 {
		opts.PollInterval = 5 * time.Minute
	}
	return &Listener{opts: opts, logger: opts.Logger}, nil
}

// Run 常驻运行监听循环，ctx 取消后优雅退出。返回 nil 表示正常停止。
func (l *Listener) Run(ctx context.Context) error {
	backoff := initialBackoff
	for {
		if ctx.Err() != nil {
			return nil
		}
		connected, err := l.session(ctx)
		if ctx.Err() != nil {
			return nil
		}
		if connected {
			backoff = initialBackoff
		}
		if err != nil {
			l.logf("IMAP 会话结束: %v；%s 后重连", err, backoff)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

// session 建立一次连接并持续 IDLE，直到出错或 ctx 取消。
// 返回值 connected 表示是否已成功登录（用于决定是否重置退避）。
func (l *Listener) session(ctx context.Context) (connected bool, err error) {
	addr := net.JoinHostPort(l.opts.Host, strconv.Itoa(l.opts.Port))
	conn, err := client.DialTLS(addr, &tls.Config{
		ServerName: l.opts.Host,
		MinVersion: tls.VersionTLS12,
	})
	if err != nil {
		return false, fmt.Errorf("连接 %s 失败: %w", addr, err)
	}
	defer conn.Logout()
	// Timeout 是 go-imap 单条命令的读写超时，也会覆盖 IDLE 期间的读操作。
	// 必须大于 IdleTimeout，否则 IDLE 会被读超时打断，形成「每 2 分钟断线重连」
	// 的抖动，还会触发服务端限流。
	conn.Timeout = l.opts.IdleTimeout + commandTimeout

	if err := conn.Login(l.opts.Username, l.opts.Password); err != nil {
		return false, fmt.Errorf("IMAP 登录失败: %w", err)
	}

	mbox, err := conn.Select(l.opts.Mailbox, true)
	if err != nil {
		return true, fmt.Errorf("选择邮箱 %q 失败: %w", l.opts.Mailbox, err)
	}
	l.initBaseline(mbox.UidNext)
	l.logf("IMAP 已连接 %s，监听 %s（只处理 UID > %d 的新邮件）", addr, l.opts.Mailbox, l.lastUID)

	updates := make(chan client.Update, 16)
	conn.Updates = updates

	poll := time.NewTicker(l.opts.PollInterval)
	defer poll.Stop()

	for {
		if ctx.Err() != nil {
			return true, ctx.Err()
		}

		stop := make(chan struct{})
		idleDone := make(chan error, 1)
		go func() {
			idleDone <- conn.Idle(stop, &client.IdleOptions{
				LogoutTimeout: l.opts.IdleTimeout,
				PollInterval:  l.opts.PollInterval,
			})
		}()

		needFetch := false
		select {
		case update := <-updates:
			if _, ok := update.(*client.MailboxUpdate); ok {
				needFetch = true
			}
		case <-poll.C:
			needFetch = true
		case err := <-idleDone:
			close(stop)
			if err != nil {
				return true, fmt.Errorf("IMAP IDLE 中断: %w", err)
			}
			return true, errors.New("IMAP IDLE 意外结束")
		case <-ctx.Done():
			close(stop)
			<-idleDone
			return true, ctx.Err()
		}

		close(stop)
		if err := <-idleDone; err != nil {
			return true, fmt.Errorf("IMAP IDLE 中断: %w", err)
		}
		if needFetch {
			if err := l.fetchNew(conn); err != nil {
				return true, fmt.Errorf("拉取新邮件失败: %w", err)
			}
		}
		drainUpdates(updates)
	}
}

// initBaseline 只在首次成功连接时用 UIDNEXT 建立水位线；后续重连保持原水位线，
// 这样断线期间到达的邮件会在重连后的第一次拉取中被补上，而不是被静默跳过。
func (l *Listener) initBaseline(uidNext uint32) {
	if l.baselineSet {
		return
	}
	if uidNext > 0 {
		l.lastUID = uidNext - 1
	}
	l.baselineSet = true
}

// fetchNew 拉取水位线之后的新邮件并逐封回调。
func (l *Listener) fetchNew(conn *client.Client) error {
	mbox, err := conn.Select(l.opts.Mailbox, true)
	if err != nil {
		return fmt.Errorf("重新选择邮箱失败: %w", err)
	}
	if mbox.UidNext == 0 || mbox.UidNext <= l.lastUID+1 {
		return nil
	}

	seqset := new(imapv1.SeqSet)
	seqset.AddRange(l.lastUID+1, 0)

	section := &imapv1.BodySectionName{Peek: true}
	items := []imapv1.FetchItem{imapv1.FetchUid, section.FetchItem()}
	messages := make(chan *imapv1.Message, 16)
	fetchDone := make(chan error, 1)
	go func() {
		// 必须用 UidFetch：SeqSet 里装的是 UID 区间，而 Fetch 按消息序号匹配，
		// 在 UID 远大于邮件总数时会静默返回空结果。
		fetchDone <- conn.UidFetch(seqset, items, messages)
	}()

	count := 0
	for msg := range messages {
		body := msg.GetBody(section)
		if body == nil {
			l.logf("UID %d 没有正文，跳过", msg.Uid)
			continue
		}
		raw, readErr := io.ReadAll(io.LimitReader(body, maxMailBytes))
		if readErr != nil {
			l.logf("读取 UID %d 失败: %v", msg.Uid, readErr)
			continue
		}
		if err := l.opts.OnMessage(msg.Uid, raw); err != nil {
			l.logf("处理 UID %d 失败: %v", msg.Uid, err)
		}
		if msg.Uid > l.lastUID {
			l.lastUID = msg.Uid
		}
		count++
	}
	if err := <-fetchDone; err != nil {
		return fmt.Errorf("FETCH 失败: %w", err)
	}
	if count > 0 {
		l.logf("已处理 %d 封新邮件（最新 UID %d）", count, l.lastUID)
	}
	return nil
}

func drainUpdates(updates <-chan client.Update) {
	for {
		select {
		case <-updates:
		default:
			return
		}
	}
}

func (l *Listener) logf(format string, args ...any) {
	if l.logger != nil {
		l.logger.Printf(format, args...)
	}
}
