package service

import (
	"context"
	"strings"
	"time"

	"typeai/internal/config"
	"typeai/internal/llm"
	"typeai/internal/session"
)

// Chat 编排一次进程内的多轮对话。
type Chat struct {
	client  *llm.Client
	store   *session.Store
	session session.Session
}

// NewChat 创建对话服务；此时只生成元数据和文件名，不写任何数据文件。
func NewChat(cfg config.Config, dataDir string, now time.Time) (*Chat, error) {
	store, err := session.New(dataDir, now)
	if err != nil {
		return nil, err
	}
	return &Chat{
		client: &llm.Client{
			BaseURL: cfg.BaseURL,
			APIKey:  cfg.APIKey,
			Model:   cfg.Model,
		},
		store: store,
		session: session.Session{
			SchemaVersion: session.SchemaVersion,
			ID:            store.ID(),
			Model:         cfg.Model,
			CreatedAt:     now,
			UpdatedAt:     now,
			Messages:      make([]session.Message, 0),
		},
	}, nil
}

// Path 返回当前 session JSON 路径。
func (c *Chat) Path() string { return c.store.Path() }

// Model 返回本次对话使用的模型名。
func (c *Chat) Model() string { return c.session.Model }

// Send 发送一轮消息。请求使用历史副本，只有 AI 完整回答且 session 落盘成功后才提交历史。
func (c *Chat) Send(ctx context.Context, input string, onDelta func(delta llm.Delta)) error {
	input = strings.TrimSpace(input)
	if input == "" {
		return coded("bad_args", "输入不能为空")
	}
	if c.client.APIKey == "" {
		return coded("not_configured", "尚未配置 API Key，请先执行 typeai config set --api-key <KEY>")
	}

	requestMessages := make([]llm.Message, 0, len(c.session.Messages)+1)
	for _, message := range c.session.Messages {
		requestMessages = append(requestMessages, llm.Message{Role: message.Role, Content: message.Content})
	}
	requestMessages = append(requestMessages, llm.Message{Role: "user", Content: input})

	answer, err := c.client.ChatStream(ctx, requestMessages, onDelta)
	if err != nil {
		if coded := asCoded(err); coded != nil {
			return coded
		}
		return coded("upstream", err.Error())
	}
	if answer == "" {
		return coded("upstream", "AI 返回了空回答")
	}

	now := time.Now()
	candidate := c.session
	candidate.UpdatedAt = now
	candidate.Messages = append(copyMessages(c.session.Messages),
		session.Message{Role: "user", Content: input, CreatedAt: now},
		session.Message{Role: "assistant", Content: answer, CreatedAt: now},
	)
	if err := c.store.Save(candidate); err != nil {
		return coded("internal", err.Error())
	}
	c.session = candidate
	return nil
}

func copyMessages(source []session.Message) []session.Message {
	out := make([]session.Message, len(source))
	copy(out, source)
	return out
}
