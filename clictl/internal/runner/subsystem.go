package runner

import (
	"encoding/binary"
	"io"
	"os"
)

// PE Optional Header 的 Subsystem 取值（Windows 仅两种常用）。
const (
	subsystemWindowsGUI = 2
	subsystemWindowsCUI = 3
)

// IsConsoleExe 判断 exe 是否为命令行（console）子系统程序。
//
// PE 结构：DOS 头 0x3C 处为 PE 偏移；COFF 头 20 字节后是 Optional Header，
// 其 Subsystem 字段位于 Optional Header + 68（PE32/PE32+ 相同），
// 即文件偏移 peOff + 24 + 68。读失败或结构不合法一律按窗口程序处理。
func IsConsoleExe(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	var dos [0x40]byte
	if _, err := io.ReadFull(f, dos[:]); err != nil {
		return false
	}
	if dos[0] != 'M' || dos[1] != 'Z' {
		return false
	}

	peOff := int64(binary.LittleEndian.Uint32(dos[0x3c:]))
	// 防御明显越界的偏移，避免对异常文件做无意义的大偏移读取。
	if peOff < 0x40 || peOff > 1<<20 {
		return false
	}

	var coff [24]byte
	if _, err := f.ReadAt(coff[:], peOff); err != nil {
		return false
	}
	if coff[0] != 'P' || coff[1] != 'E' || coff[2] != 0 || coff[3] != 0 {
		return false
	}

	var sub [2]byte
	if _, err := f.ReadAt(sub[:], peOff+24+68); err != nil {
		return false
	}
	return binary.LittleEndian.Uint16(sub[:]) == subsystemWindowsCUI
}
