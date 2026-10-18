package engine

import (
	"encoding/binary"
	"time"

	"golang.org/x/sys/windows/registry"
	"winstartupmanager/internal/model"
)

const (
	hkcuStartupApprovedRun = `Software\Microsoft\Windows\CurrentVersion\Explorer\StartupApproved\Run`
	hklmStartupApprovedRun = `Software\Microsoft\Windows\CurrentVersion\Explorer\StartupApproved\Run`
	hklmStartupApprovedRun32 = `Software\Microsoft\Windows\CurrentVersion\Explorer\StartupApproved\Run32`
)

// GetStartupApprovedPath returns the corresponding root key, subkey path, and access mask for the given source.
func GetStartupApprovedPath(source model.StartupSource) (root registry.Key, path string, isElevated bool) {
	switch source {
	case model.SourceRegistryHKCU, model.SourceRegistryHKCURunOnce:
		return registry.CURRENT_USER, hkcuStartupApprovedRun, false
	case model.SourceRegistryHKLM:
		return registry.LOCAL_MACHINE, hklmStartupApprovedRun, true
	case model.SourceRegistryHKLMWOW64:
		return registry.LOCAL_MACHINE, hklmStartupApprovedRun32, true
	default:
		return registry.CURRENT_USER, hkcuStartupApprovedRun, false
	}
}

// IsStartupApproved checks if a run entry is enabled in StartupApproved\Run.
// If not found in StartupApproved, it defaults to true (enabled).
// If byte[0] is even (0x02, 0x00, 0x06), it is enabled.
// If byte[0] is odd (0x03, 0x01), it is disabled.
func IsStartupApproved(source model.StartupSource, name string) (bool, error) {
	root, path, _ := GetStartupApprovedPath(source)
	k, err := registry.OpenKey(root, path, registry.QUERY_VALUE)
	if err != nil {
		// If the StartupApproved\Run key doesn't exist, default to true
		return true, nil
	}
	defer k.Close()

	val, _, err := k.GetBinaryValue(name)
	if err != nil {
		// Value doesn't exist, default to true
		return true, nil
	}

	return ParseStartupApprovedBytes(val), nil
}

// ParseStartupApprovedBytes evaluates the binary state according to Task Manager contract.
func ParseStartupApprovedBytes(val []byte) bool {
	if len(val) == 0 {
		return true
	}
	// Even byte 0 -> Enabled, Odd byte 0 -> Disabled
	return (val[0] % 2) == 0
}

// SetStartupApproved writes the 12-byte binary state to StartupApproved\Run.
func SetStartupApproved(source model.StartupSource, name string, enable bool) error {
	root, path, _ := GetStartupApprovedPath(source)
	k, _, err := registry.CreateKey(root, path, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()

	data := make([]byte, 12)
	if enable {
		data[0] = 0x02
	} else {
		data[0] = 0x03
		// Write Windows FILETIME in the last 8 bytes (offset 4..12)
		now := time.Now()
		ft := uint64(now.UnixNano()/100) + 116444736000000000
		binary.LittleEndian.PutUint64(data[4:], ft)
	}

	return k.SetBinaryValue(name, data)
}

// DeleteStartupApproved removes an item from StartupApproved\Run if present.
func DeleteStartupApproved(source model.StartupSource, name string) error {
	root, path, _ := GetStartupApprovedPath(source)
	k, err := registry.OpenKey(root, path, registry.SET_VALUE)
	if err != nil {
		return nil
	}
	defer k.Close()
	_ = k.DeleteValue(name)
	return nil
}