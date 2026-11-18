//go:build !windows

package quota

import "os/exec"

// hideWindow 在非 Windows 平台无对应机制。
func hideWindow(cmd *exec.Cmd) {
	_ = cmd
}
