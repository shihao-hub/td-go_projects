//go:build !dev

package env

// IsDev 默认或生产编译模式下返回 false。
func IsDev() bool {
	return false
}