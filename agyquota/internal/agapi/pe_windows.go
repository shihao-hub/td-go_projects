//go:build windows

package agapi

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"agyquota/internal/appdata"
)

// readPESubsystem 读取 PE 可执行文件的 OptionalHeader.Subsystem 字段。
// 2: IMAGE_SUBSYSTEM_WINDOWS_GUI
// 3: IMAGE_SUBSYSTEM_WINDOWS_CUI
func readPESubsystem(filePath string) (uint16, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return 0, fmt.Errorf("打开 PE 文件失败: %w", err)
	}
	defer f.Close()

	// 读取 DOS Header 中的 e_lfanew (0x3C 处，4 字节偏移)
	if _, err := f.Seek(0x3C, io.SeekStart); err != nil {
		return 0, fmt.Errorf("定位 e_lfanew 失败: %w", err)
	}
	var peOffset int32
	if err := binary.Read(f, binary.LittleEndian, &peOffset); err != nil {
		return 0, fmt.Errorf("读取 e_lfanew 失败: %w", err)
	}

	// 校验 PE 签名 ("PE\0\0" 即 0x00004550)
	if _, err := f.Seek(int64(peOffset), io.SeekStart); err != nil {
		return 0, fmt.Errorf("定位 PE 签名失败: %w", err)
	}
	var peSig uint32
	if err := binary.Read(f, binary.LittleEndian, &peSig); err != nil || peSig != 0x00004550 {
		return 0, fmt.Errorf("无效的 PE 签名: 0x%X", peSig)
	}

	// OptionalHeader 起始于 peOffset + 4 (PE Sig) + 20 (FileHeader) = peOffset + 24
	if _, err := f.Seek(int64(peOffset+24), io.SeekStart); err != nil {
		return 0, fmt.Errorf("定位 OptionalHeader 失败: %w", err)
	}
	var magic uint16
	if err := binary.Read(f, binary.LittleEndian, &magic); err != nil {
		return 0, fmt.Errorf("读取 OptionalHeader Magic 失败: %w", err)
	}

	var subsystemOffset int64
	if magic == 0x20B { // PE32+ (64 位)
		subsystemOffset = int64(peOffset + 24 + 68)
	} else if magic == 0x10B { // PE32 (32 位)
		subsystemOffset = int64(peOffset + 24 + 44)
	} else {
		return 0, fmt.Errorf("不支持的 PE Magic: 0x%X", magic)
	}

	if _, err := f.Seek(subsystemOffset, io.SeekStart); err != nil {
		return 0, fmt.Errorf("定位 Subsystem 偏移失败: %w", err)
	}
	var subsystem uint16
	if err := binary.Read(f, binary.LittleEndian, &subsystem); err != nil {
		return 0, fmt.Errorf("读取 Subsystem 失败: %w", err)
	}
	return subsystem, nil
}

// calcFileSHA256 计算文件的 SHA-256 十六进制哈希串。
func calcFileSHA256(filePath string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// isSilentCacheValid 验证静默副本缓存是否有效：
// 1. 目标文件存在且为常规非空文件；
// 2. 文件大小与源文件完全一致；
// 3. OptionalHeader.Subsystem == 2 (GUI)；
// 4. 源文件 SHA-256 绑定校验文件存在且与源文件实际哈希一致；
// 5. 修改时间戳与源文件一致。
func isSilentCacheValid(srcStat os.FileInfo, dstPath, shaPath, srcPath string) bool {
	dstStat, err := os.Stat(dstPath)
	if err != nil || dstStat.IsDir() || !dstStat.Mode().IsRegular() {
		return false
	}
	if dstStat.Size() != srcStat.Size() {
		return false
	}

	subsystem, err := readPESubsystem(dstPath)
	if err != nil || subsystem != 2 {
		return false
	}

	shaBytes, err := os.ReadFile(shaPath)
	if err != nil {
		return false
	}
	storedSHA := strings.TrimSpace(string(shaBytes))
	if storedSHA == "" {
		return false
	}

	srcSHA, err := calcFileSHA256(srcPath)
	if err != nil || !strings.EqualFold(srcSHA, storedSHA) {
		return false
	}

	if !dstStat.ModTime().Equal(srcStat.ModTime()) {
		return false
	}

	return true
}

// ensureSilentAgyExe 检查并自动生成 Subsystem 为 2 (IMAGE_SUBSYSTEM_WINDOWS_GUI) 的 agy 副本。
// 该副本存放于 %APPDATA%\language_projects\agyquota\bin\agy_silent.exe；
// Windows 11 内核在加载 GUI 子系统程序时，完全跳过控制台分配与 DefTerm 注册，从根本上杜绝新建 Tab 与抢焦点；
// 同时管道重定向（STARTF_USESTDHANDLES）仍能完整捕获其标准输出。
//
// 失败时禁止回退原始 agy.exe，必须直接返回明确错误，避免悄悄恢复到会抢焦点的路径。
func ensureSilentAgyExe(srcPath string) (string, error) {
	if srcPath == "" {
		return "", fmt.Errorf("源 agy 路径为空")
	}

	srcStat, err := os.Stat(srcPath)
	if err != nil {
		return "", fmt.Errorf("读取源 agy 文件状态失败: %w", err)
	}

	dataDir := appdata.Dir()
	if dataDir == "" {
		return "", fmt.Errorf("无法解析用户数据目录")
	}

	binDir := filepath.Join(dataDir, "bin")
	dstPath := filepath.Join(binDir, "agy_silent.exe")
	shaPath := dstPath + ".sha256"

	// 1. 严格检查多维缓存是否有效（存在、大小、Subsystem==2、源 SHA-256 绑定、修改时间）
	if isSilentCacheValid(srcStat, dstPath, shaPath, srcPath) {
		return dstPath, nil
	}

	// 2. 缓存不存在或失效，制作新缓存
	if err := os.MkdirAll(binDir, 0755); err != nil {
		return "", fmt.Errorf("创建 bin 缓存目录失败: %w", err)
	}

	tmpPath := dstPath + ".tmp"
	tmpShaPath := shaPath + ".tmp"
	defer func() {
		_ = os.Remove(tmpPath)
		_ = os.Remove(tmpShaPath)
	}()

	if err := copyAndPatchPE(srcPath, tmpPath); err != nil {
		return "", fmt.Errorf("制作静默副本失败: %w", err)
	}

	// 校验生成的临时文件 Subsystem 确实为 2
	patchedSubsystem, err := readPESubsystem(tmpPath)
	if err != nil {
		return "", fmt.Errorf("读取新生成静默副本 PE 头部失败: %w", err)
	}
	if patchedSubsystem != 2 {
		return "", fmt.Errorf("新生成静默副本 PE Subsystem 校验失败，期望 2 但实际为 %d", patchedSubsystem)
	}

	// 计算源文件 SHA-256 并写入临时哈希校验文件
	srcSHA, err := calcFileSHA256(srcPath)
	if err != nil {
		return "", fmt.Errorf("计算源 agy 文件 SHA-256 失败: %w", err)
	}
	if err := os.WriteFile(tmpShaPath, []byte(srcSHA+"\n"), 0644); err != nil {
		return "", fmt.Errorf("写入 SHA-256 校验文件失败: %w", err)
	}

	// 3. 原子覆盖可执行文件与哈希绑定文件，并对齐时间戳
	_ = os.Remove(dstPath)
	if err := os.Rename(tmpPath, dstPath); err != nil {
		return "", fmt.Errorf("替换静默可执行文件失败: %w", err)
	}

	_ = os.Remove(shaPath)
	if err := os.Rename(tmpShaPath, shaPath); err != nil {
		return "", fmt.Errorf("替换静默可执行文件哈希凭证失败: %w", err)
	}

	_ = os.Chtimes(dstPath, srcStat.ModTime(), srcStat.ModTime())

	// 最终双重验证：确保目标文件立即可读且 Subsystem 为 2
	finalSubsystem, err := readPESubsystem(dstPath)
	if err != nil || finalSubsystem != 2 {
		return "", fmt.Errorf("安装后静默副本 PE 校验异常 (subsystem=%d): %v", finalSubsystem, err)
	}

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
	} else if currentSubsystem != 2 {
		return errors.New(fmt.Sprintf("源文件 Subsystem 非预期 (当前=%d，期望 3 或 2)", currentSubsystem))
	}

	return nil
}
