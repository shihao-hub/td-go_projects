// Package browser 用系统默认浏览器打开 URL，供 GUI 与 CLI serve 共用。
package browser

import (
	"os/exec"
	"runtime"
)

// Open 打开指定 URL；调用方自行决定是否忽略错误。
func Open(url string) error {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", url}
	case "darwin":
		cmd = "open"
		args = []string{url}
	default:
		cmd = "xdg-open"
		args = []string{url}
	}
	return exec.Command(cmd, args...).Start()
}
