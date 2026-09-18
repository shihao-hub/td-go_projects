package cli

import (
	"encoding/json"
	"fmt"

	"github.com/shihao-hub/liteconf/internal/version"
)

// contract 为 schema 子命令导出的顶层契约目录。
// liteconf 无 MCP 入口（http 为纯终端透传，无自身数据面与状态，
// 见 README「定位与边界」例外记录），依据《CLI 工具开发标准》5.5 节
// 导出自身命令契约并声明 interface 为 cli。
type contract struct {
	Name      string    `json:"name"`
	Interface string    `json:"interface"`
	Version   string    `json:"version"`
	Commands  []command `json:"commands"`
}

// runSchema 输出 CLI 自身的命令契约目录（JSON），
// 不解析 PATH、不启动任何外部程序。
func runSchema(opt *Options) int {
	c := contract{
		Name:      "liteconf",
		Interface: "cli",
		Version:   version.Version,
		Commands:  commands,
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		fmt.Fprintf(opt.Stderr, "liteconf schema: 序列化契约失败：%v\n", err)
		return 1
	}
	if _, err := opt.Stdout.Write(append(data, '\n')); err != nil {
		fmt.Fprintf(opt.Stderr, "liteconf schema: 写出失败：%v\n", err)
		return 1
	}
	return 0
}
