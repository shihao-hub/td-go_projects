//go:build windows

package agapi

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// ensureSilentAgyExe 检查并自动生成 Subsystem 为 2 (IMAGE_SUBSYSTEM_WINDOWS_GUI) 的 agy 副本。
// 该副本存放于 %APPDATA%\language_projects\agyquota\bin\agy_silent.exe；
// Windows 11 内核在加载 GUI 子系统程序时，完全跳过控制台分配与 DefTerm 注册，从根本上杜绝新建 Tab 与抢焦点；
// 同时管道重定向（STARTF_USESTDHANDLES）仍能完整捕获其标准输出。
func ensureSilentAgyExe(srcPath string) (string, error) {
	if srcPath == "" {
		return "", fmt.Errorf("源 agy 路径为空")
	}

	srcStat, err := os.Stat(srcPath)
	if err != nil {
		return srcPath, fmt.Errorf("读取源 agy 文件状态失败: %w", err)
	}

	dataDir := resolveDataDir()
	if dataDir == "" {
		return srcPath, fmt.Errorf("无法解析用户数据目录")
	}

	binDir := filepath.Join(dataDir, "bin")
	dstPath := filepath.Join(binDir, "agy_silent.exe")

	// 1. 检查缓存是否有效（文件存在且大小、修改时间与源一致）
	if dstStat, err := os.Stat(dstPath); err == nil {
		if dstStat.Size() == srcStat.Size() && dstStat.ModTime().Equal(srcStat.ModTime()) {
			return dstPath, nil
		}
	}

	// 2. 缓存不存在或源文件已更新，制作新缓存
	if err := os.MkdirAll(binDir, 0755); err != nil {
		return srcPath, fmt.Errorf("创建 bin 缓存目录失败: %w", err)
	}

	tmpPath := dstPath + ".tmp"
	if err := copyAndPatchPE(srcPath, tmpPath); err != nil {
		_ = os.Remove(tmpPath)
		return srcPath, err
	}

	// 3. 原子覆盖并对齐时间戳
	_ = os.Remove(dstPath)
	if err := os.Rename(tmpPath, dstPath); err != nil {
		_ = os.Remove(tmpPath)
		return srcPath, fmt.Errorf("替换静默可执行文件失败: %w", err)
	}

	_ = os.Chtimes(dstPath, srcStat.ModTime(), srcStat.ModTime())
	return dstPath, nil
}

// copyAndPatchPE 复制并修改 PE 头的 OptionalHeader.Subsystem 字段（3 CUI -> 2 GUI）
func copyAndPatchPE(srcPath, dstPath string) error {
	srcF, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("打开源文件失败: %w", err)
	}
	defer srcF.Close()

	dstF, err := os.OpenFile(dstPath, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0755)
	if err != nil {
		return fmt.Errorf("创建目标文件失败: %w", err)
	}
	defer dstF.Close()

	if _, err := io.Copy(dstF, srcF); err != nil {
		return fmt.Errorf("复制文件数据失败: %w", err)
	}

	// 读取 DOS Header 中的 e_lfanew (0x3C 处，4 字节偏移)
	if _, err := dstF.Seek(0x3C, io.SeekStart); err != nil {
		return fmt.Errorf("定位 e_lfanew 失败: %w", err)
	}
	var peOffset int32
	if err := binary.Read(dstF, binary.LittleEndian, &peOffset); err != nil {
		return fmt.Errorf("读取 e_lfanew 失败: %w", err)
	}

	// 校验 PE 签名 ("PE\0\0" 即 0x00004550)
	if _, err := dstF.Seek(int64(peOffset), io.SeekStart); err != nil {
		return fmt.Errorf("定位 PE 签名失败: %w", err)
	}
	var peSig uint32
	if err := binary.Read(dstF, binary.LittleEndian, &peSig); err != nil || peSig != 0x00004550 {
		return fmt.Errorf("无效的 PE 文件签名: 0x%X", peSig)
	}

	// OptionalHeader 起始于 peOffset + 4 (PE Sig) + 20 (FileHeader) = peOffset + 24
	if _, err := dstF.Seek(int64(peOffset+24), io.SeekStart); err != nil {
		return fmt.Errorf("定位 OptionalHeader 失败: %w", err)
	}
	var magic uint16
	if err := binary.Read(dstF, binary.LittleEndian, &magic); err != nil {
		return fmt.Errorf("读取 OptionalHeader Magic 失败: %w", err)
	}

	var subsystemOffset int64
	if magic == 0x20B { // PE32+ (64 位)
		// Subsystem 字段位于 OptionalHeader 偏移 68 字节处
		subsystemOffset = int64(peOffset + 24 + 68)
	} else if magic == 0x10B { // PE32 (32 位)
		// Subsystem 字段位于 OptionalHeader 偏移 44 字节处
		subsystemOffset = int64(peOffset + 24 + 44)
	} else {
		return fmt.Errorf("不支持的 PE Magic: 0x%X", magic)
	}

	// 读取当前 Subsystem
	if _, err := dstF.Seek(subsystemOffset, io.SeekStart); err != nil {
		return fmt.Errorf("定位 Subsystem 偏移失败: %w", err)
	}
	var currentSubsystem uint16
	if err := binary.Read(dstF, binary.LittleEndian, &currentSubsystem); err != nil {
		return fmt.Errorf("读取 Subsystem 失败: %w", err)
	}

	// 若为 3 (CUI 控制台)，修改为 2 (GUI 图形子系统)
	if currentSubsystem == 3 {
		if _, err := dstF.Seek(subsystemOffset, io.SeekStart); err != nil {
			return fmt.Errorf("重新定位 Subsystem 失败: %w", err)
		}
		newSubsystem := uint16(2)
		if err := binary.Write(dstF, binary.LittleEndian, newSubsystem); err != nil {
			return fmt.Errorf("写入 Subsystem 失败: %w", err)
		}
	}

	return nil
}
