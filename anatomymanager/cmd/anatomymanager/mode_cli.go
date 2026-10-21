//go:build cli

package main

// consoleBuild 标明当前产物为 console 子系统（命令行优先）：
// 父 shell 会正常等待进程退出，prompt / 光标 / 管道 / 重定向全部符合预期；
// 无参数运行时打印用法，而不是拉起桌面壳。
const consoleBuild = true
