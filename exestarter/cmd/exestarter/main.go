package main

import (
	"os"

	"exestarter/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:]))
}
