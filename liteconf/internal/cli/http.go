package cli

import (
	"errors"
	"fmt"
	"os/exec"
)

// curlieName 为 http 子命令透传的目标可执行文件名。
const curlieName = "curlie"

// runHTTP 把 args 原样透传给 PATH 中的 curlie：
// 参数数组直启（不经 shell），三条标准流直通，退出码透传。
func runHTTP(args []string, opt *Options) int {
	exe, err := opt.LookPath(curlieName)
	if err != nil {
		fmt.Fprintf(opt.Stderr, "liteconf http: PATH 中未找到 %s（%v）\n请先安装：go install github.com/rs/curlie@latest\n", curlieName, err)
		return 1
	}
	cmd := exec.Command(exe, args...)
	cmd.Stdin = opt.Stdin
	cmd.Stdout = opt.Stdout
	cmd.Stderr = opt.Stderr
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			if code := exitErr.ExitCode(); code >= 0 {
				return code
			}
			fmt.Fprintf(opt.Stderr, "liteconf http: %s 被信号终止，退出码未知，按失败处理\n", curlieName)
			return 1
		}
		fmt.Fprintf(opt.Stderr, "liteconf http: 启动 %s 失败：%v\n", curlieName, err)
		return 1
	}
	return 0
}
