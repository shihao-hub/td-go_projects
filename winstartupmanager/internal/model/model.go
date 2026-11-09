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
	ID         string        `json:"id"`           // MD5(Source + Name / TaskPath)
	Source     StartupSource `json:"source"`       // 来源物理分类
	Name       string        `json:"name"`         // 注册表键名 / 任务名
	RawCommand string        `json:"raw_command"`  // 原始命令行整串
	ExePath    string        `json:"exe_path"`     // 解析后的纯净 exe 路径
	Args       string        `json:"args"`         // 启动参数
	IsEnabled  bool          `json:"is_enabled"`   // 当前是否已启用
	FileExists bool          `json:"file_exists"`  // 文件是否存在
	Publisher  string        `json:"publisher"`    // 数字签名发布商或厂商名
	IsElevated bool          `json:"is_elevated"`  // 是否需要管理员权限 (HKLM 项)

	// 计划任务扩展字段（注册表 / 启动文件夹来源为空值）
	TriggerDesc string `json:"trigger_desc,omitempty"` // 触发描述（开机时 / 登录时 / 每小时…）
	TaskPath    string `json:"task_path,omitempty"`    // 集成任务完整路径（\folder\name）
	NextRun     string `json:"next_run,omitempty"`     // 下次运行时间（空 = 未排期）
	LastRun     string `json:"last_run,omitempty"`     // 上次运行时间（空 = 从未运行）
	LastResult  int32  `json:"last_result,omitempty"`  // 上次退出码（0 = 正常）
}

func GenerateID(source StartupSource, name string) string {
	h := md5.Sum([]byte(string(source) + ":" + name))
	return hex.EncodeToString(h[:])
}