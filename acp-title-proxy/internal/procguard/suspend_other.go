//go:build !windows

package procguard

func SuspendProcess(pid int) error {
	return nil
}

func ResumeProcess(pid int) error {
	return nil
}
