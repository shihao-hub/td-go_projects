//go:build !windows

package main

import "errors"

func pickFolder(defaultDir string) (string, error) {
	return "", errors.New("当前平台不支持目录选择框，请使用 --dir 指定工作区")
}
