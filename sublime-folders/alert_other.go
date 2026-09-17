//go:build !windows

package app

import "log"

func AlertError(title, text string) {
	log.Printf("%s: %s", title, text)
}
