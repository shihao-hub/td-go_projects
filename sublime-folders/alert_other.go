//go:build !windows

package main

import "log"

func alertError(title, text string) {
	log.Printf("%s: %s", title, text)
}
