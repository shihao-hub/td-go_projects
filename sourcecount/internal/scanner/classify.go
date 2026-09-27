package scanner

import (
	"bytes"
	"io"
	"os"
	"unicode/utf8"
)

const probeBytes = 8 * 1024

// Kind 是文件内容分类。
type Kind string

const (
	KindText   Kind = "text"
	KindBinary Kind = "binary"
)

// Classify 按后缀覆盖和内容探测判定文件类型。
func Classify(path, extension string, textExtensions, binaryExtensions map[string]struct{}) (Kind, error) {
	if _, ok := textExtensions[extension]; ok {
		return KindText, nil
	}
	if _, ok := binaryExtensions[extension]; ok {
		return KindBinary, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, probeBytes))
	if err != nil {
		return "", err
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return KindBinary, nil
	}
	valid := data
	if len(valid) == probeBytes {
		// 探测被截断时，末尾可能残留多字节 UTF-8 字符的前缀；逐次丢弃
		// 最多 3 字节再校验，避免把切断的字符误判为无效 UTF-8。
		for cut := 1; cut <= 3 && cut < len(valid); cut++ {
			if utf8.Valid(valid[:len(valid)-cut]) {
				valid = valid[:len(valid)-cut]
				break
			}
		}
	}
	if !utf8.Valid(valid) {
		return KindBinary, nil
	}
	return KindText, nil
}
