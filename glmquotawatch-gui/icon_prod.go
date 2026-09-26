//go:build !dev

package main

import _ "embed"

//go:embed build/windows/icon.ico
var iconICO []byte