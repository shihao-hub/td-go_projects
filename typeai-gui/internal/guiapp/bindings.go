// bindings.go：前端绑定服务——启动/写入/尺寸同步/重启终端会话，
// 并把 PTY 输出以 base64 事件推给前端（JSON 事件通道下保证字节保真，
// 规避 UTF-8 多字节字符被 chunk 边界截断的问题）。
package guiapp

import (
	"encoding/base64"
	"errors"
	"sync"

	"typeai-gui/internal/terminal"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// 事件名常量（前端 Events.On 使用同一组名字）。
const (
	EventTerminalData = "terminal:data"
	EventTerminalExit = "terminal:exit"
	EventTerminalErr  = "terminal:error"
)

// TerminalService 前端可调用方法的宿主（wails3 生成 TS bindings）。
type TerminalService struct {
	app *application.App

	mu       sync.Mutex
	sess     *terminal.Session
	gen      int    // 会话代号：Restart 后旧会话的退出事件不再作用于新界面
	path     string // 已解析的 typeai.exe 路径
	startErr string // 最近一次启动失败的说明（State 透出）
}

// TerminalState 前端状态视图。
type TerminalState struct {
	Gen        int    `json:"gen"`
	TypeaiPath string `json:"typeaiPath"`
	Running    bool   `json:"running"`
	StartError string `json:"startError"`
}

// Bind 注入应用实例（app.Run 之前调用）。
func (t *TerminalService) Bind(app *application.App) { t.app = app }

// Start 启动 typeai 终端会话；已有会话时幂等返回。
// cols/rows 为前端 xterm 初始尺寸。
func (t *TerminalService) Start(cols, rows int) error {
	t.mu.Lock()
	if t.sess != nil {
		t.mu.Unlock()
		return nil
	}
	t.mu.Unlock()

	path, err := LocateTypeai()
	if err != nil {
		t.mu.Lock()
		t.startErr = err.Error()
		t.mu.Unlock()
		t.emitError(err.Error())
		return err
	}

	sess, err := terminal.Start(path, cols, rows)
	if err != nil {
		t.mu.Lock()
		t.startErr = err.Error()
		t.mu.Unlock()
		t.emitError(err.Error())
		return err
	}

	t.mu.Lock()
	t.gen++
	gen := t.gen
	t.path = path
	t.startErr = ""
	t.sess = sess
	t.mu.Unlock()

	sess.OnData = t.emitData
	sess.OnExit = func(code int, err error) { t.onExit(gen, code) }
	return nil
}

// Write 将前端键盘输入转发到 PTY。
func (t *TerminalService) Write(data string) error {
	sess, err := t.current()
	if err != nil {
		return err
	}
	return sess.Write([]byte(data))
}

// Resize 同步终端尺寸（列, 行）。
func (t *TerminalService) Resize(cols, rows int) error {
	sess, err := t.current()
	if err != nil {
		return err
	}
	return sess.Resize(cols, rows)
}

// Restart 关闭当前会话并按新尺寸重启（前端会话已结束时可用）。
func (t *TerminalService) Restart(cols, rows int) error {
	t.mu.Lock()
	sess := t.sess
	t.sess = nil
	t.mu.Unlock()
	if sess != nil {
		sess.Close() // 旧会话退出事件带旧 gen，前端自动忽略
	}
	return t.Start(cols, rows)
}

// StopSession 终止当前会话（关窗钩子调用；同时导出给前端）。
func (t *TerminalService) StopSession() {
	t.mu.Lock()
	sess := t.sess
	t.sess = nil
	t.mu.Unlock()
	if sess != nil {
		sess.Close()
	}
}

// State 返回当前会话状态（前端挂载时初始化界面用）。
func (t *TerminalService) State() TerminalState {
	t.mu.Lock()
	defer t.mu.Unlock()
	return TerminalState{
		Gen:        t.gen,
		TypeaiPath: t.path,
		Running:    t.sess != nil,
		StartError: t.startErr,
	}
}

// current 取当前活跃会话（未启动时报错）。
func (t *TerminalService) current() (*terminal.Session, error) {
	t.mu.Lock()
	sess := t.sess
	t.mu.Unlock()
	if sess == nil {
		return nil, errors.New("终端会话未启动")
	}
	return sess, nil
}

// emitData PTY 输出 → base64 事件（读 goroutine 串行回调）。
func (t *TerminalService) emitData(chunk []byte) {
	if t.app == nil {
		return
	}
	t.app.Event.Emit(EventTerminalData, base64.StdEncoding.EncodeToString(chunk))
}

// onExit 会话退出事件（gen 防串扰：旧会话的退出不影响新界面）。
func (t *TerminalService) onExit(gen, code int) {
	if t.app == nil {
		return
	}
	t.app.Event.Emit(EventTerminalExit, map[string]int{"gen": gen, "code": code})
}

func (t *TerminalService) emitError(msg string) {
	if t.app == nil {
		return
	}
	t.app.Event.Emit(EventTerminalErr, map[string]string{"message": msg})
}
