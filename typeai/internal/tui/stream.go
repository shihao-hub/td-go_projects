package tui

import (
	"context"

	"typeai/internal/llm"
	"typeai/internal/service"

	tea "github.com/charmbracelet/bubbletea"
)

type sender interface {
	Send(msg tea.Msg)
}

type streamDeltaMsg struct {
	Kind llm.DeltaKind
	Text string
}

type streamResultMsg struct {
	Err error
}

type elapsedTickMsg struct{}

func startStream(commandSender sender, chat *service.Chat, ctx context.Context, input string) tea.Cmd {
	return func() tea.Msg {
		err := chat.Send(ctx, input, func(delta llm.Delta) {
			commandSender.Send(streamDeltaMsg{Kind: delta.Kind, Text: delta.Text})
		})
		return streamResultMsg{Err: err}
	}
}
