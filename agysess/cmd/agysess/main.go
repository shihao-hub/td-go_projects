package main

import (
	"os"

	"agysess/internal/cli"
	"agysess/internal/gui"
	"agysess/internal/service"
)

var Version = "1.0.0"

func main() {
	exitCode := cli.Execute(Version, func(svc *service.SessionService) error {
		return gui.RunGUI(svc)
	})
	os.Exit(exitCode)
}
