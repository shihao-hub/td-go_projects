package tui

import "typeai/internal/service"

type clipboardImageMsg struct {
	Image service.Image
	Err   error
}
