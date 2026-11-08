package app

import "strings"

// silentStartFlags 是触发“静默驻留托盘”的启动开关。
// 开机自启项注册的正是 --hidden，--tray 为同义别名。
var silentStartFlags = map[string]struct{}{
	"--hidden": {},
	"--tray":   {},
}

// IsSilentStart 判断命令行参数是否要求静默启动（只驻留托盘、不建窗）。
//
// args 需为完整命令行（args[0] 是可执行文件路径），它只作为路径解释，
// 永远不参与开关解析，避免 exe 路径中恰好含 --hidden 字样时误判。
//
// 该判断被两处共用，必须保持单一事实源：
//   - main 入口：决定本次进程是否直接建窗；
//   - 单实例回调：决定“重复的静默启动”要不要唤醒已驻留的主窗口。
func IsSilentStart(args []string) bool {
	for i, arg := range args {
		if i == 0 {
			continue
		}
		if _, ok := silentStartFlags[strings.ToLower(strings.TrimSpace(arg))]; ok {
			return true
		}
	}
	return false
}
