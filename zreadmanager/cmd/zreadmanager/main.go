package main

import (
	"os"

	"zreadmanager/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:]))
}
