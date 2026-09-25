package main

import (
	"os"

	"typeai/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:]))
}
