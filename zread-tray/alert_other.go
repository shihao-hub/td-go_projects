//go:build !windows

package main

import (
	"fmt"
	"os"
)

func alertError(title, text string) {
	fmt.Fprintf(os.Stderr, "%s: %s\n", title, text)
}
