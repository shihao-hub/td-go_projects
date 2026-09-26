//go:build dev

package env

// IsDev 在编译参数包含 -tags dev 时返回 true。
func IsDev() bool {
	return true
}