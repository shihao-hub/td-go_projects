package engine

import (
	"fmt"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modVersion                = windows.NewLazyDLL("version.dll")
	procGetFileVersionInfoSize = modVersion.NewProc("GetFileVersionInfoSizeW")
	procGetFileVersionInfo     = modVersion.NewProc("GetFileVersionInfoW")
	procVerQueryValue          = modVersion.NewProc("VerQueryValueW")
)

// GetFilePublisher returns the company name or publisher for the specified executable.
func GetFilePublisher(exePath string) string {
	if strings.TrimSpace(exePath) == "" {
		return "无"
	}

	expanded, ok := resolveExePath(exePath)
	if !ok {
		return "文件不存在"
	}

	pathPtr, err := syscall.UTF16PtrFromString(expanded)
	if err != nil {
		return "无数字签名"
	}

	var dummy uintptr
	size, _, _ := procGetFileVersionInfoSize.Call(uintptr(unsafe.Pointer(pathPtr)), uintptr(unsafe.Pointer(&dummy)))
	if size == 0 {
		return "无数字签名"
	}

	buf := make([]byte, size)
	ret, _, _ := procGetFileVersionInfo.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		0,
		size,
		uintptr(unsafe.Pointer(&buf[0])),
	)
	if ret == 0 {
		return "无数字签名"
	}

	// Try standard translation blocks: 040904B0 (US English Unicode), 080404B0 (Chinese PRC Unicode), etc.
	translations := []string{
		"040904B0", // US English, Unicode
		"080404B0", // Chinese PRC, Unicode
		"040904E4", // US English, Multilingual
		"080404E4", // Chinese PRC, Multilingual
		"000004B0", // Neutral, Unicode
	}

	var transBlock uintptr
	var transLen uint32
	subBlockPtr, _ := syscall.UTF16PtrFromString(`\VarFileInfo\Translation`)
	r, _, _ := procVerQueryValue.Call(
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(subBlockPtr)),
		uintptr(unsafe.Pointer(&transBlock)),
		uintptr(unsafe.Pointer(&transLen)),
	)
	if r != 0 && transLen >= 4 {
		type LANGANDCODEPAGE struct {
			wLanguage uint16
			wCodePage uint16
		}
		lp := (*LANGANDCODEPAGE)(unsafe.Pointer(transBlock))
		dynamicBlock := fmt.Sprintf("%04x%04x", lp.wLanguage, lp.wCodePage)
		translations = append([]string{dynamicBlock}, translations...)
	}

	for _, trans := range translations {
		company := queryValueString(&buf[0], fmt.Sprintf(`\StringFileInfo\%s\CompanyName`, trans))
		if company != "" {
			return company
		}
		product := queryValueString(&buf[0], fmt.Sprintf(`\StringFileInfo\%s\ProductName`, trans))
		if product != "" {
			return product
		}
	}

	return "无数字签名"
}

func queryValueString(block *byte, subBlock string) string {
	subBlockPtr, err := syscall.UTF16PtrFromString(subBlock)
	if err != nil {
		return ""
	}

	var resPtr uintptr
	var resLen uint32
	r, _, _ := procVerQueryValue.Call(
		uintptr(unsafe.Pointer(block)),
		uintptr(unsafe.Pointer(subBlockPtr)),
		uintptr(unsafe.Pointer(&resPtr)),
		uintptr(unsafe.Pointer(&resLen)),
	)
	if r == 0 || resLen == 0 || resPtr == 0 {
		return ""
	}

	chars := (*[1024]uint16)(unsafe.Pointer(resPtr))
	var u16 []uint16
	for i := 0; i < int(resLen) && chars[i] != 0; i++ {
		u16 = append(u16, chars[i])
	}
	return syscall.UTF16ToString(u16)
}