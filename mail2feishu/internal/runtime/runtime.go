// Package runtime 组装配置、IMAP 监听、过滤与投递，供 GUI 与 CLI 共用。
//
// Runtime 是进程内唯一的服务所有者：启动时创建 Listener 与 Pipeline，
// 停止时按「取消监听 → 等待退出 → 关闭 Pipeline」的顺序收尾。
package runtime

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"mail2feishu/internal/audit"
	"mail2feishu/internal/buildinfo"
	"mail2feishu/internal/config"
	"mail2feishu/internal/feishu"
	imaplistener "mail2feishu/internal/imap"
	"mail2feishu/internal/matcher"
	"mail2feishu/internal/service"
	"mail2feishu/internal/state"
)

// Options 是 Runtime 的路径与日志配置。
type Options struct {
	ConfigPath string
	DataDir    string
	Logger     *log.Logger
}

// Runtime 持有配置、状态、审计与服务生命周期。
type Runtime struct {
	opts   Options
	logger *log.Logger

	mu        sync.Mutex
	cfg       *config.Config
	state     *state.Store
	audit     *audit.Logger
	pipeline  *service.Pipeline
	listener  *imaplistener.Listener
	ctx       context.Context
	cancel    context.CancelFunc
	done      chan struct{}
	running   bool
	startedAt time.Time

	connected  bool
	lastMailAt time.Time
	lastPushAt time.Time
	lastPushOK bool
	lastError  string
	configErr  error
}

// New 加载配置并打开状态与审计文件，但尚未开始监听。
func New(opts Options) (*Runtime, error) {
	if opts.ConfigPath == "" {
		return nil, errors.New("配置路径不能为空")
	}
	if opts.DataDir == "" {
		return nil, errors.New("数据目录不能为空")
	}
	if opts.Logger == nil {
		opts.Logger = log.New(os.Stderr, "", log.LstdFlags)
	}

	cfg, configErr := config.Load(opts.ConfigPath)
	if configErr != nil {
		// 首次启动或配置损坏时不阻塞 GUI：用空配置打开界面，
		// 由用户在配置页补齐后保存，再启动监听。
		cfg = &config.Config{}
		cfg.ApplyDefaults()
	}
	stateStore, err := state.Open(filepath.Join(opts.DataDir, "state.json"))
	if err != nil {
		opts.Logger.Printf("读取状态文件失败，将重建 UID 水位线: %v", err)
	}
	auditLogger, err := audit.OpenDefault(opts.DataDir)
	if err != nil {
		return nil, fmt.Errorf("初始化审计日志失败: %w", err)
	}
	return &Runtime{
		opts:      opts,
		logger:    opts.Logger,
		cfg:       cfg,
		state:     stateStore,
		audit:     auditLogger,
		configErr: configErr,
	}, nil
}

// Start 启动后台监听；重复调用安全。
func (r *Runtime) Start() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running {
		return nil
	}
	return r.startLocked()
}

func (r *Runtime) startLocked() error {
	if r.configErr != nil {
		return r.configErr
	}
	cfg := r.cfg
	whitelist, err := matcher.New(cfg.Whitelist)
	if err != nil {
		return fmt.Errorf("白名单配置无效: %w", err)
	}
	pipeline, err := service.New(service.Options{
		Matcher:        whitelist,
		Feishu:         feishu.NewClient(cfg.Feishu.WebhookURL, cfg.Feishu.Secret),
		DebounceWindow: cfg.Ratelimit.DebounceWindow.Std(),
		ThreadCooldown: cfg.Ratelimit.ThreadCooldown.Std(),
		Auditor:        r.audit,
		OnPush:         r.onPush,
		Logger:         r.logger,
	})
	if err != nil {
		return fmt.Errorf("初始化业务内核失败: %w", err)
	}
	listener, err := imaplistener.New(imaplistener.Options{
		Host:           cfg.IMAP.Host,
		Port:           cfg.IMAP.Port,
		Username:       cfg.IMAP.Username,
		Password:       cfg.IMAP.Password,
		Mailbox:        cfg.IMAP.Mailbox,
		IdleTimeout:    cfg.IMAP.IdleTimeout.Std(),
		PollInterval:   cfg.IMAP.PollInterval.Std(),
		State:          r.state,
		OnMessage:      r.handleMail,
		OnConnected:    r.onConnected,
		OnDisconnected: r.onDisconnected,
		Logger:         r.logger,
	})
	if err != nil {
		return fmt.Errorf("初始化 IMAP 监听器失败: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	r.ctx, r.cancel, r.done = ctx, cancel, done
	r.pipeline, r.listener = pipeline, listener
	r.running = true
	r.startedAt = time.Now()
	r.connected = false
	r.lastError = ""

	go func() {
		defer close(done)
		if err := listener.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			r.setError(err)
			r.logger.Printf("监听器退出: %v", err)
		}
	}()
	return nil
}

// Stop 停止监听并等待在途处理结束；重复调用安全。
func (r *Runtime) Stop() {
	r.mu.Lock()
	if !r.running {
		r.mu.Unlock()
		return
	}
	cancel, done, pipeline := r.cancel, r.done, r.pipeline
	r.running = false
	r.connected = false
	r.mu.Unlock()

	cancel()
	<-done
	pipeline.Close()
}

// Reconnect 用当前配置重启监听。
func (r *Runtime) Reconnect() error {
	r.Stop()
	return r.Start()
}

// Close 停止监听并关闭审计文件。
func (r *Runtime) Close() {
	r.Stop()
	if err := r.audit.Close(); err != nil {
		r.logger.Printf("关闭审计日志失败: %v", err)
	}
}

// Config 返回脱敏后的配置视图。
func (r *Runtime) Config() ConfigView {
	r.mu.Lock()
	defer r.mu.Unlock()
	return NewConfigView(r.cfg)
}

// UpdateConfig 保存配置并重启监听。password / secret 留空表示沿用当前值。
func (r *Runtime) UpdateConfig(view ConfigView) error {
	next, err := view.ToConfig()
	if err != nil {
		return err
	}

	r.mu.Lock()
	current := r.cfg
	r.mu.Unlock()

	if next.IMAP.Password == "" {
		next.IMAP.Password = current.IMAP.Password
	}
	if next.Feishu.Secret == "" {
		next.Feishu.Secret = current.Feishu.Secret
	}
	if next.Feishu.WebhookURL == "" {
		next.Feishu.WebhookURL = current.Feishu.WebhookURL
	}
	// 仅当测试发信仍处于启用状态时沿用旧授权码；清空 username 视作关闭该功能，
	// 避免旧凭证被重新写回配置文件。
	if next.TestSender.Password == "" && strings.TrimSpace(next.TestSender.Username) != "" {
		next.TestSender.Password = current.TestSender.Password
	}
	next.ApplyDefaults()

	if err := next.Save(r.opts.ConfigPath); err != nil {
		return err
	}

	r.mu.Lock()
	r.cfg = next
	r.configErr = nil
	r.mu.Unlock()
	return r.Reconnect()
}

// TestWebhook 使用当前配置发送一张测试卡片。
func (r *Runtime) TestWebhook(ctx context.Context) error {
	r.mu.Lock()
	cfg := r.cfg
	r.mu.Unlock()

	client := feishu.NewClient(cfg.Feishu.WebhookURL, cfg.Feishu.Secret)
	return client.SendCard(ctx, feishu.CardOptions{
		Title:    "mail2feishu 测试卡片",
		Template: feishu.TemplateGreen,
		Subject:  "配置验证成功",
		Summary:  "如果你看到这张卡片，说明飞书 Webhook 配置可用。",
	})
}

// Status 返回界面展示用的运行状态快照。
type Status struct {
	Running    bool      `json:"running"`
	Connected  bool      `json:"connected"`
	Version    string    `json:"version"`
	Mode       string    `json:"mode"`
	ConfigPath string    `json:"config_path"`
	DataDir    string    `json:"data_dir"`
	Username   string    `json:"username"`
	Mailbox    string    `json:"mailbox"`
	LastUID    uint32    `json:"last_uid"`
	Rules      int       `json:"rules"`
	StartedAt  time.Time `json:"started_at"`
	LastMailAt time.Time `json:"last_mail_at"`
	LastPushAt time.Time `json:"last_push_at"`
	LastPushOK bool      `json:"last_push_ok"`
	LastError  string    `json:"last_error"`
}

// Status 返回当前状态。
func (r *Runtime) Status() Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	snapshot := r.state.Snapshot()
	lastError := r.lastError
	if lastError == "" && r.configErr != nil {
		lastError = r.configErr.Error()
	}
	return Status{
		Running:    r.running,
		Connected:  r.connected,
		Version:    buildinfo.Version,
		Mode:       buildinfo.Mode,
		ConfigPath: r.opts.ConfigPath,
		DataDir:    r.opts.DataDir,
		Username:   r.cfg.IMAP.Username,
		Mailbox:    r.cfg.IMAP.Mailbox,
		LastUID:    snapshot.LastUID,
		Rules:      len(r.cfg.Whitelist),
		StartedAt:  r.startedAt,
		LastMailAt: r.lastMailAt,
		LastPushAt: r.lastPushAt,
		LastPushOK: r.lastPushOK,
		LastError:  lastError,
	}
}

// ConfigPath 返回配置文件路径。
func (r *Runtime) ConfigPath() string { return r.opts.ConfigPath }

// DataDir 返回数据目录。
func (r *Runtime) DataDir() string { return r.opts.DataDir }

func (r *Runtime) handleMail(uid uint32, raw []byte) error {
	r.mu.Lock()
	r.lastMailAt = time.Now()
	pipeline := r.pipeline
	r.mu.Unlock()
	if pipeline == nil {
		return errors.New("服务尚未启动")
	}
	err := pipeline.HandleMail(uid, raw)
	if err != nil {
		r.setError(err)
	}
	return err
}

func (r *Runtime) onPush(ok bool, err error) {
	r.mu.Lock()
	r.lastPushAt = time.Now()
	r.lastPushOK = ok
	if err != nil {
		r.lastError = err.Error()
	}
	r.mu.Unlock()
}

func (r *Runtime) onConnected(addr string) {
	r.mu.Lock()
	r.connected = true
	r.lastError = ""
	r.mu.Unlock()
	r.logger.Printf("IMAP 连接就绪: %s", addr)
}

func (r *Runtime) onDisconnected(err error) {
	r.mu.Lock()
	r.connected = false
	if err != nil && !errors.Is(err, context.Canceled) {
		r.lastError = err.Error()
	}
	r.mu.Unlock()
}

func (r *Runtime) setError(err error) {
	if err == nil {
		return
	}
	r.mu.Lock()
	r.lastError = err.Error()
	r.mu.Unlock()
}
