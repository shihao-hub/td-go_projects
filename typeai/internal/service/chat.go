package service

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"typeai/internal/config"
	"typeai/internal/llm"
	"typeai/internal/session"
)

// Chat 编排一次进程内的多轮对话。
type Chat struct {
	client     *llm.Client
	store      *session.Store
	session    session.Session
	imageMu    sync.Mutex
	imageCache map[imageCacheKey]Image
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
		store:      store,
		imageCache: make(map[imageCacheKey]Image),
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
	return c.SendWithImages(ctx, input, nil, onDelta)
}

// SendWithImages 发送一轮可携带本地图的消息；历史副本在请求前重建。
func (c *Chat) SendWithImages(ctx context.Context, input string, stagedImages []Image, onDelta func(delta llm.Delta)) error {
	input = strings.TrimSpace(input)
	requestText, markerImages, err := ParseImageMarkers(input)
	if err != nil {
		return err
	}
	images := make([]Image, 0, len(stagedImages)+len(markerImages))
	images = append(images, stagedImages...)
	images = append(images, markerImages...)
	if len(images) > MaxImagesPerMessage {
		return coded("invalid_image", fmt.Sprintf("每条消息最多 %d 张图片", MaxImagesPerMessage))
	}
	if strings.TrimSpace(requestText) == "" && len(images) == 0 {
		return coded("bad_args", "输入不能为空")
	}
	if c.client.APIKey == "" {
		return coded("not_configured", "尚未配置 API Key，请先执行 typeai config set --api-key <KEY>")
	}

	requestMessages, err := c.buildRequestMessages(requestText, images)
	if err != nil {
		if operation := asCoded(err); operation != nil {
			return operation
		}
		return coded("invalid_image", err.Error())
	}

	answer, err := c.client.ChatStream(ctx, requestMessages, onDelta)
	if err != nil {
		if operation := asCoded(err); operation != nil {
			return operation
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
		session.Message{Role: "user", Content: input, Images: imageMetadata(images), CreatedAt: now},
		session.Message{Role: "assistant", Content: answer, CreatedAt: now},
	)
	if err := c.store.Save(candidate); err != nil {
		return coded("internal", err.Error())
	}
	c.session = candidate
	return nil
}

func (c *Chat) buildRequestMessages(input string, currentImages []Image) ([]llm.Message, error) {
	messages := make([]llm.Message, 0, len(c.session.Messages)+1)
	for _, message := range c.session.Messages {
		if len(message.Images) == 0 {
			messages = append(messages, llm.TextMessage(message.Role, message.Content))
			continue
		}
		images := make([]llm.Image, 0, len(message.Images))
		for _, stored := range message.Images {
			image, err := c.historyImage(stored)
			if err != nil {
				return nil, err
			}
			images = append(images, image)
		}
		messages = append(messages, llm.ImageMessage(message.Role, message.Content, images))
	}

	if len(currentImages) == 0 {
		messages = append(messages, llm.TextMessage("user", input))
		return messages, nil
	}
	images := make([]llm.Image, 0, len(currentImages))
	for _, image := range currentImages {
		images = append(images, llm.Image{Base64Data: image.Base64, MediaType: image.MediaType})
		c.cacheImage(image)
	}
	messages = append(messages, llm.ImageMessage("user", input, images))
	return messages, nil
}

func copyMessages(source []session.Message) []session.Message {
	out := make([]session.Message, len(source))
	copy(out, source)
	return out
}

type imageCacheKey struct {
	Path      string
	Size      int64
	MediaType string
}

func (c *Chat) cacheImage(image Image) {
	c.imageMu.Lock()
	defer c.imageMu.Unlock()
	c.imageCache[imageCacheKey{
		Path:      image.Path,
		Size:      image.Size,
		MediaType: image.MediaType,
	}] = image
}

func (c *Chat) cachedImage(stored session.Image) (Image, bool) {
	c.imageMu.Lock()
	defer c.imageMu.Unlock()
	image, ok := c.imageCache[imageCacheKey{
		Path:      stored.Path,
		Size:      stored.Size,
		MediaType: stored.MediaType,
	}]
	return image, ok
}

func (c *Chat) historyImage(stored session.Image) (llm.Image, error) {
	if image, ok := c.cachedImage(stored); ok {
		return llm.Image{Base64Data: image.Base64, MediaType: image.MediaType}, nil
	}
	image, err := loadImage(stored.Path, stored.Size, stored.MediaType)
	if err != nil {
		return llm.Image{}, fmt.Errorf("重建历史图片 %s 失败: %w", stored.Path, err)
	}
	c.cacheImage(Image{
		Path:      stored.Path,
		FileName:  stored.FileName,
		MediaType: stored.MediaType,
		Size:      stored.Size,
		Base64:    image.Base64Data,
	})
	return image, nil
}
