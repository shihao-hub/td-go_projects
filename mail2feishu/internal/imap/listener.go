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
	dialTimeout    = 30 * time.Second
	maxMailBytes   = 10 << 20 // 单封邮件读取上限 10 MiB
)

// StateStore 持久化已处理邮件的 UID 水位线。
type StateStore interface {
	LastUID(username string, uidValidity uint32) (uint32, bool)
	Save(username string, uidValidity, lastUID uint32) error
}

// Options 是监听器的连接与回调配置。
type Options struct {
	Host         string
	Port         int
	Username     string
	Password     string
	Mailbox      string
	IdleTimeout  time.Duration
	PollInterval time.Duration
	// State 可选：跨进程持久化水位线；nil 时退化为纯内存。
	State StateStore
	// OnMessage 在收到新邮件时调用，raw 为完整 RFC 5322 原文。
	// 返回错误表示这封邮件没有被安全接管（例如落盘失败），监听器不会推进
	// UID 水位线，并会在下一轮重连时重试同一条及之后的邮件。
	OnMessage func(uid, uidValidity uint32, raw []byte) error
	// OnConnected 在登录并选中邮箱成功后调用。
	OnConnected func(addr string)
	// OnDisconnected 在会话结束时调用，err 为结束原因。
	OnDisconnected func(err error)
	Logger         *log.Logger
}

// Listener 是 IMAP IDLE 监听器。
type Listener struct {
	opts   Options
	logger *log.Logger

	// lastUID 是已处理邮件的 UID 水位线。它必须在重连之间保持，
	// 否则每次重连都会把断线期间到达的邮件当成历史邮件跳过。
	lastUID     uint32
	uidValidity uint32
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
	conn, rawConn, err := l.dial(ctx, addr)
	if err != nil {
		return false, err
	}
	defer conn.Logout()

	// ctx 取消时主动断开底层连接：IDLE 期间协程可能长时间阻塞在读上，
	// 仅靠 cancel 无法让 session 及时退出，退出/重连会因此卡住。
	stopWatch := make(chan struct{})
	defer close(stopWatch)
	go func() {
		select {
		case <-ctx.Done():
			_ = rawConn.Close()
		case <-stopWatch:
		}
	}()

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
	l.syncMailbox(mbox)
	l.logf("IMAP 已连接 %s，监听 %s（只处理 UID > %d 的新邮件）", addr, l.opts.Mailbox, l.lastUID)
	if l.opts.OnConnected != nil {
		l.opts.OnConnected(addr)
	}
	defer func() {
		if l.opts.OnDisconnected != nil {
			l.opts.OnDisconnected(err)
		}
	}()

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

// dial 建立带超时的 TLS 连接，并返回底层连接供取消时强制关闭。
func (l *Listener) dial(ctx context.Context, addr string) (*client.Client, net.Conn, error) {
	dialer := &net.Dialer{Timeout: dialTimeout, KeepAlive: 30 * time.Second}
	rawConn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, nil, fmt.Errorf("连接 %s 失败: %w", addr, err)
	}
	tlsConn := tls.Client(rawConn, &tls.Config{
		ServerName: l.opts.Host,
		MinVersion: tls.VersionTLS12,
	})
	// 握手阶段同样需要截止时间：服务端在 greeting 阶段不响应时必须能超时退出。
	if err := tlsConn.SetDeadline(time.Now().Add(dialTimeout)); err != nil {
		_ = rawConn.Close()
		return nil, nil, fmt.Errorf("设置 TLS 超时失败: %w", err)
	}
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		_ = rawConn.Close()
		return nil, nil, fmt.Errorf("TLS 握手失败: %w", err)
	}
	if err := tlsConn.SetDeadline(time.Time{}); err != nil {
		_ = rawConn.Close()
		return nil, nil, fmt.Errorf("清除 TLS 超时失败: %w", err)
	}
	conn, err := client.New(tlsConn)
	if err != nil {
		_ = tlsConn.Close()
		return nil, nil, fmt.Errorf("初始化 IMAP 客户端失败: %w", err)
	}
	return conn, tlsConn, nil
}

// syncMailbox 在每次选中邮箱后同步 UIDVALIDITY：
// 运行期间邮箱被重建（UIDVALIDITY 变化）时，旧 UID 对新邮箱没有意义，
// 必须重建基线，否则新邮件会被旧水位线跳过。
func (l *Listener) syncMailbox(mbox *imapv1.MailboxStatus) {
	if l.baselineSet && l.uidValidity != 0 && mbox.UidValidity != l.uidValidity {
		l.logf("UIDVALIDITY 变化（%d → %d），重建 UID 水位线；outbox 中未投递邮件继续投递",
			l.uidValidity, mbox.UidValidity)
		l.baselineSet = false
		l.lastUID = 0
	}
	l.uidValidity = mbox.UidValidity
	l.initBaseline(mbox.UidNext)
}

// initBaseline 只在首次成功连接（或 UIDVALIDITY 变化）时建立水位线：
//   - 优先使用持久化的水位线（账号与 UIDVALIDITY 都匹配），这样进程重启后
//     停机期间到达的邮件会被补拉；
//   - 否则以当前 UIDNEXT-1 为基线（首次运行或账号/邮箱已变化）。
//
// 后续重连保持原水位线，断线期间的邮件同样会被补上。
func (l *Listener) initBaseline(uidNext uint32) {
	if l.baselineSet {
		return
	}
	if l.opts.State != nil {
		if uid, ok := l.opts.State.LastUID(l.opts.Username, l.uidValidity); ok {
			l.lastUID = uid
			l.baselineSet = true
			l.logf("从持久化水位线 UID %d 继续（UIDVALIDITY=%d）", uid, l.uidValidity)
			return
		}
	}
	if uidNext > 0 {
		l.lastUID = uidNext - 1
	}
	l.baselineSet = true
	l.logf("建立新水位线 UID %d（UIDVALIDITY=%d）", l.lastUID, l.uidValidity)
	l.persistLastUID()
}

// persistLastUID 把当前水位线写回持久化存储；失败只告警，不中断监听。
func (l *Listener) persistLastUID() {
	if l.opts.State == nil {
		return
	}
	if err := l.opts.State.Save(l.opts.Username, l.uidValidity, l.lastUID); err != nil {
		l.logf("保存 UID 水位线失败: %v", err)
	}
}

// fetchNew 拉取水位线之后的新邮件并逐封回调。
//
// 水位线只推进到「已确认接管」的最后一封：任何一封处理失败都会立即停止
// 本轮推进，并返回错误触发重连，避免失败邮件被永久跳过。
func (l *Listener) fetchNew(conn *client.Client) error {
	mbox, err := conn.Select(l.opts.Mailbox, true)
	if err != nil {
		return fmt.Errorf("重新选择邮箱失败: %w", err)
	}
	l.syncMailbox(mbox)
	// 同一 UIDVALIDITY 下 UID 单调递增；水位线超出 UIDNEXT 说明邮箱被
	// 重建或服务器异常，重置基线避免永久跳过后续邮件。
	if mbox.UidNext > 0 && l.lastUID >= mbox.UidNext {
		l.logf("水位线 UID %d 超出 UIDNEXT=%d，重置为 UIDNEXT-1", l.lastUID, mbox.UidNext)
		l.lastUID = mbox.UidNext - 1
		l.persistLastUID()
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
	advanced := false
	var handlerErr error
	for msg := range messages {
		// 已发生错误：继续消费剩余消息避免 FETCH 协程阻塞，但不再推进水位线，
		// 也不处理后续邮件——UID 水位线是单值，越过失败点就会丢信。
		if handlerErr != nil {
			continue
		}
		body := msg.GetBody(section)
		if body == nil {
			handlerErr = fmt.Errorf("UID %d 没有正文", msg.Uid)
			continue
		}
		raw, readErr := io.ReadAll(io.LimitReader(body, maxMailBytes))
		if readErr != nil {
			handlerErr = fmt.Errorf("读取 UID %d 失败: %w", msg.Uid, readErr)
			continue
		}
		if err := l.opts.OnMessage(msg.Uid, l.uidValidity, raw); err != nil {
			handlerErr = fmt.Errorf("处理 UID %d 失败: %w", msg.Uid, err)
			continue
		}
		if msg.Uid > l.lastUID {
			l.lastUID = msg.Uid
			advanced = true
		}
		count++
	}
	if err := <-fetchDone; err != nil {
		if advanced {
			l.persistLastUID()
		}
		return fmt.Errorf("FETCH 失败: %w", err)
	}
	if advanced {
		l.persistLastUID()
	}
	if count > 0 {
		l.logf("已入队 %d 封新邮件（最新 UID %d）", count, l.lastUID)
	}
	return handlerErr
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
