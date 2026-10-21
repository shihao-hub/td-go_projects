//go:build !dev

package main

import _ "embed"

//go:embed build/icon.ico
var iconICO []byte
