package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// main（练习版入口，go run ./cmd/practice）
// 需求：
//  1. 定时记录 sublimetext 左侧 Folders 的目录内容和数量（可能需要考虑多标签怎么办，第一期只考虑）
//  2. 先终端实现，而后拓展为托盘触发
//
// 面向过程与面向对象分析法：
//
//	找到 sublimetext 的数据存储区返回 Folders 的目录内容和数量等信息
//
// 说明：本文件自包含（自己手写核心链路），与根目录 app 包互不影响；
// 想偷懒调用现成实现时 import "sublime-folders" 即可（见 app.LoadAutoSession / app.CurrentFolders）。
func main() {
	session, err := getSublimeTextSession()
	if err != nil {
		panic(fmt.Errorf("获取 Sublime Text 会话失败: %w", err))
	}
	if len(session.Windows) == 0 {
		panic("Auto Save Session 中没有 windows 记录，无法获取 Folders 信息")
	}
	printSideBarInfo(session)
}

func printSideBarInfo(session SublimeSession) {
	var sublimesideBarInfo SublimeSideBarInfo
	sublimesideBarInfo.windowId = 0
	sublimesideBarInfo.folders = session.Windows[0].Folders
	fmt.Println("sublimesideBarInfo: ", sublimesideBarInfo)
	// sh-todo: 上面只是例子，实则我需要的很简单，cli 展示即可
	fmt.Printf("Sublime Text 共 %d 个窗口\n", len(session.Windows))
	for i, win := range session.Windows {
		fmt.Println("──────────────────────────────")
		fmt.Printf("窗口 %d（folders 共 %d 个）\n", i, len(win.Folders))
		if len(win.Folders) == 0 {
			fmt.Println("  (无 folders)")
			continue
		}
		for j, f := range win.Folders {
			fmt.Printf("  %d. %s\n", j+1, f.Path)
		}
	}
}

func getSublimeTextSession() (SublimeSession, error) {
	// 读取 %APPDATA%\Sublime Text\Local\Auto Save Session.sublime_session 文件解析成 json
	// 取出来 windows 字段（记得判空）
	var session SublimeSession
	// 1. 拼路径
	userConfigDir, err := os.UserConfigDir()
	if err != nil {
		return session, fmt.Errorf("获取用户配置目录失败: %w", err)
	}
	targetFilePath := filepath.Join(userConfigDir, "Sublime Text", "Local", "Auto Save Session.sublime_session")
	// 2. 读文件
	data, err := os.ReadFile(targetFilePath)
	if err != nil {
		return session, fmt.Errorf("读取会话文件 %s 失败: %w", targetFilePath, err)
	}
	// 3. 解析 JSON 填充 struct
	if err := json.Unmarshal(data, &session); err != nil {
		return session, fmt.Errorf("解析会话文件 %s 失败: %w", targetFilePath, err)
	}
	return session, nil
}

// Auto Save Session.sublime_session 的数据结构（只取部分）
type SublimeFolder struct {
	Path string `json:"path"`
}

type SublimeWindow struct {
	Folders []SublimeFolder `json:"folders"`
	Project string          `json:"project"`
}

type SublimeSession struct {
	Windows []SublimeWindow `json:"windows"`
}

type SublimeSideBarInfo struct {
	folders  []SublimeFolder
	windowId int
}
