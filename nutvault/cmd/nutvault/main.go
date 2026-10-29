//go:build !cli

package main

import (
	"fmt"
	"net"
	"os"

	"nutvault/internal/api"
	"nutvault/internal/app"
	"nutvault/internal/storage"
)

func main() {
	defer func() {
		if r := recover(); r != nil {
			app.LogPanic(r)
		}
	}()

	hidden := false
	for _, arg := range os.Args[1:] {
		switch arg {
		case "--hidden", "--tray":
			hidden = true
		}
	}

	if err := app.LaunchGUI("", 0, hidden); err != nil {
		fallbackServe(!hidden)
	}
}

func fallbackServe(openBrowser bool) {
	dbPath := app.DefaultDBPath()
	store, err := storage.NewStore(dbPath)
	if err != nil {
		return
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return
	}

	server := api.NewServer(store, api.Meta{
		Channel:      app.Channel(),
		BuildMode:    app.BuildMode,
		Version:      app.Version,
		DBPath:       dbPath,
		IsAutoStart:  app.IsAutoStartEnabled,
		SetAutoStart: app.SetAutoStart,
	})

	url := api.URL(listener)
	if openBrowser {
		fmt.Printf("WebUI 已在后台启动: %s\n", url)
	}
	_ = server.Serve(listener)
}
