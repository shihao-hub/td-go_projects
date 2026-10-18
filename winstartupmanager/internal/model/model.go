package model

import (
	"crypto/md5"
	"encoding/hex"
)

type StartupSource string

const (
	SourceRegistryHKCU        StartupSource = "HKCU_Run"
	SourceRegistryHKCURunOnce   StartupSource = "HKCU_RunOnce"
	SourceRegistryHKLM        StartupSource = "HKLM_Run"
	SourceRegistryHKLMWOW64   StartupSource = "HKLM_WOW6432Node"
	SourceStartupFolderUser   StartupSource = "Folder_User"
	SourceStartupFolderCommon StartupSource = "Folder_Common"
	SourceTaskScheduler       StartupSource = "TaskScheduler"
)

type StartupItem struct {
	ID         string        `json:"id"`          // MD5(Source + Name)
	Source     StartupSource `json:"source"`      // 来源物理分类
	Name       string        `json:"name"`        // 注册表键名 / 任务名
	RawCommand string        `json:"raw_command"` // 原始命令行整串
	ExePath    string        `json:"exe_path"`    // 解析后的纯净 exe 路径
	Args       string        `json:"args"`        // 启动参数
	IsEnabled  bool          `json:"is_enabled"`  // 当前是否已启用
	FileExists bool          `json:"file_exists"` // 文件是否存在
	Publisher  string        `json:"publisher"`   // 数字签名发布商或厂商名
	IsElevated bool          `json:"is_elevated"` // 是否需要管理员权限 (HKLM 项)
}

func GenerateID(source StartupSource, name string) string {
	h := md5.Sum([]byte(string(source) + ":" + name))
	return hex.EncodeToString(h[:])
}