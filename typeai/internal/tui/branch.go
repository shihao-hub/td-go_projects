package tui

import (
	"context"
	"time"

	"typeai/internal/service"
	"typeai/internal/session"
)

// branchState 保存单个分支在 TUI 运行时的独立状态。
type branchState struct {
	ID           string
	ParentID     string
	ForkAt       int // fork_message_index: 继承父分支历史前缀的消息数量
	Messages     []uiMessage
	Active       *activeTurn
	Running      bool
	Status       string
	OperationErr error
	Cancel       context.CancelFunc
	DraftInput   string
	Staged       []service.Image
	CreatedAt    time.Time
}

func newBranchState(id, parentID string, forkAt int, messages []uiMessage, now time.Time) *branchState {
	return &branchState{
		ID:        id,
		ParentID:  parentID,
		ForkAt:    forkAt,
		Messages:  messages,
		Status:    "ready",
		CreatedAt: now,
	}
}

// resolveBranchUIMessages 递归解析指定分支的历史 UI 消息。
func resolveBranchUIMessages(branchID string, branches map[string]*branchState) []uiMessage {
	b, ok := branches[branchID]
	if !ok {
		return nil
	}
	if b.ParentID == "" {
		out := make([]uiMessage, len(b.Messages))
		copy(out, b.Messages)
		return out
	}
	parentHistory := resolveBranchUIMessages(b.ParentID, branches)
	forkAt := b.ForkAt
	if forkAt < 0 {
		forkAt = 0
	}
	if forkAt > len(parentHistory) {
		forkAt = len(parentHistory)
	}
	prefix := parentHistory[:forkAt]
	out := make([]uiMessage, len(prefix)+len(b.Messages))
	copy(out, prefix)
	copy(out[len(prefix):], b.Messages)
	return out
}

func sessionMessagesToUIMessages(messages []session.Message) []uiMessage {
	out := make([]uiMessage, len(messages))
	for i, m := range messages {
		images := make([]service.Image, len(m.Images))
		for j, img := range m.Images {
			images[j] = service.Image{
				Path:      img.Path,
				FileName:  img.FileName,
				MediaType: img.MediaType,
				Size:      img.Size,
			}
		}
		out[i] = uiMessage{
			Role:    m.Role,
			Content: m.Content,
			Images:  images,
		}
	}
	return out
}
