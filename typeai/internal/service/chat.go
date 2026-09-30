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
	dataDir    string
	session    session.Session
	hasSaved   bool
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
		dataDir:    dataDir,
		imageCache: make(map[imageCacheKey]Image),
		session: session.Session{
			SchemaVersion:  session.SchemaVersion,
			ID:             store.ID(),
			Model:          cfg.Model,
			CreatedAt:      now,
			UpdatedAt:      now,
			RootBranchID:   session.DefaultRootBranchID,
			ActiveBranchID: session.DefaultRootBranchID,
			Branches: []session.Branch{
				{
					ID:               session.DefaultRootBranchID,
					ParentID:         "",
					ForkMessageIndex: 0,
					Messages:         make([]session.Message, 0),
					CreatedAt:        now,
				},
			},
			Messages: make([]session.Message, 0),
		},
	}, nil
}

// Path 返回当前 session JSON 路径。
func (c *Chat) Path() string { return c.store.Path() }

// Model 返回本次对话使用的模型名。
func (c *Chat) Model() string { return c.session.Model }

// Session 返回当前会话快照。
func (c *Chat) Session() session.Session { return c.session }

// AddBranch 添加一个新分支；如果当前会话已落盘，则立即原子保存。
func (c *Chat) AddBranch(b session.Branch) error {
	candidate := c.session
	candidate.Branches = append(copyBranches(candidate.Branches), b)
	candidate.ActiveBranchID = b.ID
	candidate.UpdatedAt = b.CreatedAt

	if c.hasSaved {
		if err := c.store.Save(candidate); err != nil {
			return coded("internal", err.Error())
		}
	}
	c.session = candidate
	return nil
}

// SetActiveBranch 切换激活分支；如果当前会话已落盘，则立即原子保存。
func (c *Chat) SetActiveBranch(branchID string) error {
	if _, ok := session.GetBranch(branchID, c.session.Branches); !ok {
		return coded("bad_args", fmt.Sprintf("分支 %s 不存在", branchID))
	}
	if c.session.ActiveBranchID == branchID {
		return nil
	}
	candidate := c.session
	candidate.ActiveBranchID = branchID
	candidate.UpdatedAt = time.Now()

	if c.hasSaved {
		if err := c.store.Save(candidate); err != nil {
			return coded("internal", err.Error())
		}
	}
	c.session = candidate
	return nil
}

// DeleteBranch 删除指定分支。根分支或存在子分支的分支不能删除。
func (c *Chat) DeleteBranch(branchID string) error {
	branchID = strings.TrimSpace(branchID)
	if branchID == "" {
		return coded("bad_args", "分支 ID 不能为空")
	}
	if branchID == c.session.RootBranchID {
		return coded("bad_args", "无法删除根分支 "+branchID)
	}

	target, ok := session.GetBranch(branchID, c.session.Branches)
	if !ok {
		return coded("bad_args", fmt.Sprintf("分支 %s 不存在", branchID))
	}

	for _, b := range c.session.Branches {
		if b.ParentID == branchID {
			return coded("bad_args", fmt.Sprintf("无法删除分支 %s: 该分支存在子分支 %s，请先删除子分支", branchID, b.ID))
		}
	}

	candidate := c.session
	newBranches := make([]session.Branch, 0, len(candidate.Branches)-1)
	for _, b := range candidate.Branches {
		if b.ID != branchID {
			newBranches = append(newBranches, b)
		}
	}
	candidate.Branches = newBranches

	if candidate.ActiveBranchID == branchID {
		candidate.ActiveBranchID = target.ParentID
		if candidate.ActiveBranchID == "" || !hasBranch(candidate.ActiveBranchID, candidate.Branches) {
			candidate.ActiveBranchID = candidate.RootBranchID
		}
	}
	candidate.UpdatedAt = time.Now()

	if c.hasSaved {
		if err := c.store.Save(candidate); err != nil {
			return coded("internal", err.Error())
		}
	}
	c.session = candidate
	return nil
}

// RenameBranch 重命名指定分支，并级联更新依赖该分支的子分支 ParentID。
func (c *Chat) RenameBranch(oldID, newName string) error {
	oldID = strings.TrimSpace(oldID)
	newName = strings.TrimSpace(newName)
	if oldID == "" || newName == "" {
		return coded("bad_args", "分支名不能为空")
	}
	if oldID == newName {
		return nil
	}
	if !isValidBranchName(newName) {
		return coded("bad_args", "分支名不合法: 仅支持字母、数字、下划线、减号和点号 (1-32字符)")
	}
	if _, ok := session.GetBranch(newName, c.session.Branches); ok {
		return coded("bad_args", fmt.Sprintf("分支 %s 已存在", newName))
	}
	if _, ok := session.GetBranch(oldID, c.session.Branches); !ok {
		return coded("bad_args", fmt.Sprintf("分支 %s 不存在", oldID))
	}

	candidate := c.session
	newBranches := make([]session.Branch, len(candidate.Branches))
	for i, b := range candidate.Branches {
		nb := b
		if nb.ID == oldID {
			nb.ID = newName
		}
		if nb.ParentID == oldID {
			nb.ParentID = newName
		}
		newBranches[i] = nb
	}
	candidate.Branches = newBranches

	if candidate.ActiveBranchID == oldID {
		candidate.ActiveBranchID = newName
	}
	if candidate.RootBranchID == oldID {
		candidate.RootBranchID = newName
	}
	candidate.UpdatedAt = time.Now()

	if c.hasSaved {
		if err := c.store.Save(candidate); err != nil {
			return coded("internal", err.Error())
		}
	}
	c.session = candidate
	return nil
}

func hasBranch(id string, branches []session.Branch) bool {
	_, ok := session.GetBranch(id, branches)
	return ok
}

func isValidBranchName(name string) bool {
	if len(name) < 1 || len(name) > 32 {
		return false
	}
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.') {
			return false
		}
	}
	return true
}

// Resume 根据 session ID 恢复会话，替换 Store 和 session 状态。
func (c *Chat) Resume(sessionID string) (*session.Session, error) {
	newStore, loadedSess, err := session.LoadByID(c.dataDir, sessionID)
	if err != nil {
		return nil, coded("bad_args", err.Error())
	}

	c.store = newStore
	c.session = *loadedSess
	c.hasSaved = true
	c.imageMu.Lock()
	c.imageCache = make(map[imageCacheKey]Image)
	c.imageMu.Unlock()
	return loadedSess, nil
}

// SessionSummaries 返回可选择的持久化会话摘要，并隐藏当前进程所在会话。
func (c *Chat) SessionSummaries() ([]session.SessionSummary, error) {
	summaries, err := session.ListSessions(c.dataDir)
	if err != nil {
		return nil, coded("internal", err.Error())
	}

	selectable := make([]session.SessionSummary, 0, len(summaries))
	for _, summary := range summaries {
		if summary.ID != c.session.ID {
			selectable = append(selectable, summary)
		}
	}
	return selectable, nil
}

// Send 发送一轮消息。请求使用历史副本，只有 AI 完整回答且 session 落盘成功后才提交历史。
func (c *Chat) Send(ctx context.Context, input string, onDelta func(delta llm.Delta)) error {
	return c.SendWithImages(ctx, input, nil, onDelta)
}

// SendWithImages 发送一轮可携带本地图的消息；历史副本在请求前重建。
func (c *Chat) SendWithImages(ctx context.Context, input string, stagedImages []Image, onDelta func(delta llm.Delta)) error {
	return c.SendBranch(ctx, c.session.ActiveBranchID, input, stagedImages, onDelta)
}

// SendBranch 在指定分支上发送一轮消息，请求使用该分支解析后的历史上下文。
func (c *Chat) SendBranch(ctx context.Context, branchID string, input string, stagedImages []Image, onDelta func(delta llm.Delta)) error {
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

	resolvedHistory, err := session.ResolveMessages(branchID, c.session.Branches)
	if err != nil {
		return coded("internal", fmt.Sprintf("解析分支历史失败: %v", err))
	}

	requestMessages, err := c.buildRequestMessagesFromHistory(resolvedHistory, requestText, images)
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
	// 复制分支切片，避免保存失败时共享底层数组污染 c.session 的内存历史
	candidate.Branches = copyBranches(candidate.Branches)

	targetIdx := -1
	for i, b := range candidate.Branches {
		if b.ID == branchID {
			targetIdx = i
			break
		}
	}
	if targetIdx < 0 {
		return coded("internal", "未找到目标分支")
	}

	candidate.Branches[targetIdx].Messages = append(
		copyMessages(candidate.Branches[targetIdx].Messages),
		session.Message{Role: "user", Content: input, Images: imageMetadata(images), CreatedAt: now},
		session.Message{Role: "assistant", Content: answer, CreatedAt: now},
	)
	candidate.ActiveBranchID = branchID

	if err := c.store.Save(candidate); err != nil {
		return coded("internal", err.Error())
	}
	c.session = candidate
	c.hasSaved = true
	return nil
}

func (c *Chat) buildRequestMessagesFromHistory(history []session.Message, input string, currentImages []Image) ([]llm.Message, error) {
	messages := make([]llm.Message, 0, len(history)+1)
	for _, message := range history {
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

func copyBranches(source []session.Branch) []session.Branch {
	out := make([]session.Branch, len(source))
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
