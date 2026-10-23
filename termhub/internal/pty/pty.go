// Package pty 是终端伪控制台的抽象层。
// 抽出接口是为了让 SessionManager 在单测中注入 fake PTY，真 ConPTY 只在人工验收时使用。
package pty

// Term 创建的终端会话句柄：可读写流、可等待退出、可改尺寸。
// 接口保持最小面，fake 实现无需启动任何进程。
type Term interface {
	// Read 从 PTY 主侧读终端输出（含 VT 转义序列），EOF/错误表示会话结束。
	Read(p []byte) (int, error)
	// Write 向 PTY 写入用户键盘输入。
	Write(p []byte) (int, error)
	// Resize 通知 PTY 前端视口尺寸变化。
	Resize(cols, rows int) error
	// Close 关闭 PTY 并终止所附着进程，返回退出码。
	Close() error
	// Pid 附着进程的 PID（fake 可返回 0）。
	Pid() int
	// MemoryBytes 附着进程当前工作集（fake 返回 0）。
	MemoryBytes() uint64
}

// Factory 依据启动参数创建一个 Term。
// SessionManager 只依赖此接口，真 ConPTY 工厂与 fake 工厂可互换。
type Factory interface {
	Start(opts Options) (Term, error)
}

// Options 一个终端会话的启动参数。
type Options struct {
	// CommandLine 完整命令行（第一版固定为 powershell.exe）。
	CommandLine string
	// WorkDir 初始工作目录；空则由 shell 决定。
	WorkDir string
	// InitialCommand 拉起后自动执行的一条命令（启动项使用）。
	InitialCommand string
	// Cols/Rows 初始视口尺寸。
	Cols, Rows int
	// Env 额外注入的环境变量（如 PSReadLine 历史 HistorySavePath）。
	Env []string
	// Source 会话来源标识：manual / 启动项 id（T02 起使用）。
	Source string
}

// CloseWaiter 关闭并等待进程退出返回退出码的语义。
// ConPTY Close 在 Win10/11 上会连带终止附着进程，fake 实现自行模拟。
