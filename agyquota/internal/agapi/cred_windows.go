//go:build windows

package agapi

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"syscall"
	"unsafe"
)

var (
	modAdvapi32  = syscall.NewLazyDLL("advapi32.dll")
	procCredRead = modAdvapi32.NewProc("CredReadW")
	procCredFree = modAdvapi32.NewProc("CredFree")
)

type credential struct {
	flags              uint32
	credType           uint32
	targetName         *uint16
	comment            *uint16
	lastWritten        syscall.Filetime
	credentialBlobSize uint32
	credentialBlob     *byte
	persist            uint32
	attributeCount     uint32
	attributes         uintptr
	targetAlias        *uint16
	userName           *uint16
}

// GetAgyLoggedEmail 从 Windows 凭据管理器读取 gemini:antigravity 并解析其 id_token 获取邮箱
func GetAgyLoggedEmail() string {
	target, err := syscall.UTF16PtrFromString("gemini:antigravity")
	if err != nil {
		return ""
	}
	var credPtr *credential
	r1, _, _ := procCredRead.Call(
		uintptr(unsafe.Pointer(target)),
		1, // CRED_TYPE_GENERIC
		0,
		uintptr(unsafe.Pointer(&credPtr)),
	)
	if r1 == 0 || credPtr == nil {
		return ""
	}
	defer procCredFree.Call(uintptr(unsafe.Pointer(credPtr)))

	if credPtr.credentialBlobSize == 0 || credPtr.credentialBlob == nil {
		return ""
	}
	blob := unsafe.Slice(credPtr.credentialBlob, credPtr.credentialBlobSize)

	var data struct {
		IDToken string `json:"id_token"`
	}
	if err := json.Unmarshal(blob, &data); err != nil || data.IDToken == "" {
		return ""
	}

	return parseEmailFromJWT(data.IDToken)
}

func parseEmailFromJWT(jwtStr string) string {
	parts := strings.Split(jwtStr, ".")
	if len(parts) < 2 {
		return ""
	}
	payload := parts[1]
	switch len(payload) % 4 {
	case 2:
		payload += "=="
	case 3:
		payload += "="
	}
	b, err := base64.URLEncoding.DecodeString(payload)
	if err != nil {
		return ""
	}
	var claims struct {
		Email string `json:"email"`
	}
	_ = json.Unmarshal(b, &claims)
	return claims.Email
}
