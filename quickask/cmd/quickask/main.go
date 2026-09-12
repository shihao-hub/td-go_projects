package main

import (
	"os"

	"quickask/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:]))
}
