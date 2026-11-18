package main

import (
	"os"

	"ccquota/internal/cli"
	"ccquota/internal/version"
)

func main() {
	opt := &cli.Options{
		Stdout:  os.Stdout,
		Stderr:  os.Stderr,
		DBPath:  os.Getenv("CCQUOTA_DB"),
		Version: version.Version,
	}
	os.Exit(cli.Run(os.Args[1:], opt))
}
