// Package tui implements the interactive terminal UI shell.
package tui

import (
	"errors"
	"fmt"
	"io"
	"os"

	"typeai/internal/service"

	tea "github.com/charmbracelet/bubbletea"
)

const tuiFPS = 12

// Run starts the single-process interactive TUI. The chat service owns all
// business behavior; this package only renders state and forwards input.
func Run(chat *service.Chat, stdout io.Writer) error {
	if !isTerminal(os.Stdin) || !isTerminalWriter(stdout) {
		return errors.New("交互模式需要 TTY；请直接在终端中运行 typeai")
	}

	renderer := newMarkdownRenderer(styleName(colorEnabled(stdout)))
	model := newModel(chat, renderer, colorEnabled(stdout), 80, 24)
	program := tea.NewProgram(
		model,
		tea.WithAltScreen(),
		tea.WithFPS(tuiFPS),
		tea.WithOutput(stdout),
	)
	model.sender = program

	if _, err := program.Run(); err != nil {
		return fmt.Errorf("运行 TUI 失败: %w", err)
	}
	return nil
}

func colorEnabled(stdout io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	return isTerminalWriter(stdout)
}

func isTerminal(file *os.File) bool {
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func isTerminalWriter(writer io.Writer) bool {
	file, ok := writer.(*os.File)
	if !ok {
		return false
	}
	return isTerminal(file)
}
