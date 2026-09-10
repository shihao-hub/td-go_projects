package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "copy_launcher: "+format+"\n", args...)
	os.Exit(1)
}

// projectRoot 基于本文件源码路径定位项目根：main.go → copy_launcher → scripts → python-launcher-go。
// go run 下 os.Executable() 指向临时构建目录、CWD 随调用方式变化（go -C 时是脚本目录），
// 源码绝对路径是唯一稳定锚点。
func projectRoot() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("cannot locate source file via runtime.Caller")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(file))), nil
}

func locateLauncherExe() (string, error) {
	root, err := projectRoot()
	if err != nil {
		return "", err
	}
	exe := filepath.Join(root, "launcher.exe")
	info, err := os.Stat(exe)
	if err != nil {
		return "", fmt.Errorf("launcher.exe not found: %s (run .\\build.ps1 first)", exe)
	}
	if info.IsDir() {
		return "", fmt.Errorf("launcher.exe is a directory: %s", exe)
	}
	return exe, nil
}

// copyLauncherExe 流式复制源文件到 dst（io.Copy 内部 32KB buffer，内存占用与文件大小无关）。
func copyLauncherExe(dst string) error {
	src, err := locateLauncherExe()
	if err != nil {
		return err
	}
	mode := os.FileMode(0o666)
	if info, err := os.Stat(src); err == nil {
		mode = info.Mode().Perm()
	}
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open %s: %w", src, err)
	}
	defer srcFile.Close()

	dstFile, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return fmt.Errorf("create %s: %w", dst, err)
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return fmt.Errorf("copy %s -> %s: %w", src, dst, err)
	}
	return nil
}

// askName 交互确定目标文件名：[1] 目标目录名 [2] 自定义 [3] launcher.exe
func askName(stdin *bufio.Reader, targetDir string) (string, error) {
	dirName := filepath.Base(targetDir)
	for {
		fmt.Printf("exe 名字：[1] %s.exe（目录名）  [2] 自定义  [3] launcher.exe（默认）\n", dirName)
		fmt.Print("选择 [1/2/3]: ")
		choice, err := stdin.ReadString('\n')
		if err != nil {
			return "", err
		}
		switch strings.TrimSpace(choice) {
		case "1":
			return dirName + ".exe", nil
		case "2":
			for {
				fmt.Print("输入名字（带不带 .exe 均可）: ")
				line, err := stdin.ReadString('\n')
				if err != nil {
					return "", err
				}
				name := strings.TrimSpace(line)
				if name == "" {
					fmt.Println("名字不能为空，请重新输入")
					continue
				}
				if !strings.EqualFold(filepath.Ext(name), ".exe") {
					name += ".exe"
				}
				return name, nil
			}
		case "3":
			return "launcher.exe", nil
		default:
			fmt.Println("无效选择，请输入 1 / 2 / 3")
		}
	}
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: go -C .\\scripts\\copy_launcher\\ run . <目标目录>")
		os.Exit(2)
	}
	targetDir := os.Args[1]

	info, err := os.Stat(targetDir)
	if err != nil || !info.IsDir() {
		fail("target directory not found: %s", targetDir)
	}

	stdin := bufio.NewReader(os.Stdin)
	name, err := askName(stdin, targetDir)
	if err != nil {
		fail("%v", err)
	}
	dst := filepath.Join(targetDir, name)

	if _, err := os.Stat(dst); err == nil {
		fmt.Printf("%s 已存在，overwrite? [y/N]: ", dst)
		ans, err := stdin.ReadString('\n')
		if err != nil || !strings.EqualFold(strings.TrimSpace(ans), "y") {
			fail("canceled")
		}
	}

	if err := copyLauncherExe(dst); err != nil {
		fail("%v", err)
	}
	fmt.Printf("copied: %s\n", dst)
}
