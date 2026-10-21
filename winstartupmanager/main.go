package main

import (
	"embed"
	"os"

	"winstartupmanager/internal/guiapp"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	hidden := false
	for _, arg := range os.Args[1:] {
		if arg == "--hidden" || arg == "--tray" {
			hidden = true
		}
	}

	guiapp.Run(guiapp.RunOptions{
		Hidden: hidden,
		Assets: assets,
		Icon:   iconICO,
	})
}
