package scanner

import (
	"bufio"
	"io"
	"os"

	"sourcecount/internal/contract"
)

// CountTextFile 流式统计文本文件的 Unicode code point 和逻辑行数。
func CountTextFile(path string) (contract.TextStats, error) {
	file, err := os.Open(path)
	if err != nil {
		return contract.TextStats{}, err
	}
	defer file.Close()
	return CountText(file)
}

// CountText 统计 UTF-8 文本。ReadRune 对无效字节返回 RuneError/size=1，
// 因此强制文本时每个无效字节会计为一个替换字符。
func CountText(reader io.Reader) (contract.TextStats, error) {
	br := bufio.NewReaderSize(reader, 64*1024)
	var stats contract.TextStats
	lineHasContent := false
	pendingCR := false

	for {
		r, _, err := br.ReadRune()
		if err == io.EOF {
			break
		}
		if err != nil {
			return contract.TextStats{}, err
		}
		stats.Chars++

		if pendingCR {
			if r == '\n' {
				stats.Lines++
				lineHasContent = false
				pendingCR = false
				continue
			}
			stats.Lines++
			lineHasContent = false
			pendingCR = false
		}

		switch r {
		case '\r':
			pendingCR = true
		case '\n':
			stats.Lines++
			lineHasContent = false
		default:
			lineHasContent = true
		}
	}

	if pendingCR {
		stats.Lines++
		lineHasContent = false
	}
	if lineHasContent {
		stats.Lines++
	}
	return stats, nil
}
