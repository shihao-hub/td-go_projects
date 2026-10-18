package main

import (
	"embed"
	"os"

	"winstartupmanager/internal/guiapp"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/icon.ico
var iconICO []byte

func main() {
	hidden := false
	for _, arg := range os.Args[1:] {
		if arg == "--hidden" {
			hidden = true
		}
	}

	guiapp.Run(guiapp.RunOptions{
		Hidden: hidden,
		Assets: assets,
		Icon:   iconICO,
	})
}