package main

import (
	"runtime/debug"

	"exe-launcher/internal/ui"
)

func main() {
	ui.InitLogging()
	defer func() {
		if r := recover(); r != nil {
			ui.LogFatal("PANIC in main: %v\n%s", r, debug.Stack())
		}
	}()
	ui.Run()
}
