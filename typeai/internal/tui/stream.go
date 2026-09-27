package tui

import (
	"context"
	"time"

	"typeai/internal/llm"
	"typeai/internal/service"

	tea "github.com/charmbracelet/bubbletea"
)

type sender interface {
	Send(msg tea.Msg)
}

type streamDeltaMsg struct {
	BranchID string
	TurnID   string
	Kind     llm.DeltaKind
	Text     string
}

type streamResultMsg struct {
	BranchID string
	TurnID   string
	Err      error
}

type elapsedTickMsg struct{}

func scheduleElapsedTick() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg {
		return elapsedTickMsg{}
	})
}

func startStream(commandSender sender, chat *service.Chat, ctx context.Context, branchID, turnID, input string) tea.Cmd {
	return startStreamWithImages(commandSender, chat, ctx, branchID, turnID, input, nil)
}

func startStreamWithImages(commandSender sender, chat *service.Chat, ctx context.Context, branchID, turnID, input string, images []service.Image) tea.Cmd {
	return func() tea.Msg {
		err := chat.SendBranch(ctx, branchID, input, images, func(delta llm.Delta) {
			commandSender.Send(streamDeltaMsg{
				BranchID: branchID,
				TurnID:   turnID,
				Kind:     delta.Kind,
				Text:     delta.Text,
			})
		})
		return streamResultMsg{
			BranchID: branchID,
			TurnID:   turnID,
			Err:      err,
		}
	}
}
