//go:build windows

package hotkey

import (
	"fmt"
	"runtime"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32                 = windows.NewLazySystemDLL("user32.dll")
	kernel32               = windows.NewLazySystemDLL("kernel32.dll")
	procRegisterHotKey     = user32.NewProc("RegisterHotKey")
	procUnregisterHotKey   = user32.NewProc("UnregisterHotKey")
	procGetMessageW        = user32.NewProc("GetMessageW")
	procPostThreadMessageW = user32.NewProc("PostThreadMessageW")
	procGetCurrentThreadId = kernel32.NewProc("GetCurrentThreadId")
)

const (
	WM_QUIT     = 0x0012
	WM_HOTKEY   = 0x0312
	MOD_ALT     = 0x0001
	MOD_CONTROL = 0x0002
	MOD_SHIFT   = 0x0004

	hotkeyID = 1
)

// msg 对齐必须匹配 Win64 MSG（48 字节）。
type msg struct {
	HWnd    windows.HWND
	Message uint32
	_       uint32 // padding
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      struct{ X, Y int32 }
}

// Manager 管理当前唯一注册的全局热键；Set 可在运行期换键。
type Manager struct {
	mu sync.Mutex

	running bool
	stopped chan struct{} // 消息循环退出信号
	tid     uint32
}

func NewManager() *Manager { return &Manager{} }

// Set 注册（或换绑）全局热键；cb 在热键线程回调，务必快速返回（UI 内用 fyne.Do 转发）。
// 热键被其他程序占用时返回错误。combo 无效或 cb 为 nil 同样报错。
func (m *Manager) Set(combo Combo, cb func()) error {
	if cb == nil {
		return fmt.Errorf("hotkey: callback is nil")
	}
	if !combo.Valid() {
		return fmt.Errorf("hotkey: invalid combo %s", combo)
	}
	vk, ok := vkCode(combo.Key)
	if !ok {
		return fmt.Errorf("hotkey: unsupported key %s", combo.Key)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopLocked() // 旧循环先退出

	mods := 0
	if combo.Alt {
		mods |= MOD_ALT
	}
	if combo.Ctrl {
		mods |= MOD_CONTROL
	}
	if combo.Shift {
		mods |= MOD_SHIFT
	}

	registered := make(chan error, 1)
	m.stopped = make(chan struct{})
	m.running = true

	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		tid, _, _ := procGetCurrentThreadId.Call()
		m.mu.Lock()
		m.tid = uint32(tid)
		m.mu.Unlock()

		ret, _, err := procRegisterHotKey.Call(0, hotkeyID, uintptr(mods), uintptr(vk))
		if ret == 0 {
			registered <- fmt.Errorf("热键 %s 注册失败（可能已被其他程序占用）: %w", combo, err)
			close(m.stopped)
			m.mu.Lock()
			m.running = false
			m.mu.Unlock()
			return
		}
		registered <- nil

		for {
			var message msg
			r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0)
			if r == 0 || r == ^uintptr(0) { // WM_QUIT 或错误
				break
			}
			if message.Message == WM_HOTKEY && message.WParam == hotkeyID {
				cb()
			}
		}
		procUnregisterHotKey.Call(0, hotkeyID)
		close(m.stopped)
		m.mu.Lock()
		m.running = false
		m.mu.Unlock()
	}()

	return <-registered
}

// Clear 注销热键并停止消息循环。
func (m *Manager) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopLocked()
}

func (m *Manager) stopLocked() {
	if !m.running {
		return
	}
	tid := m.tid
	stopped := m.stopped
	m.mu.Unlock()
	if tid != 0 {
		procPostThreadMessageW.Call(uintptr(tid), WM_QUIT, 0, 0)
	}
	select {
	case <-stopped:
	case <-time.After(2 * time.Second): // 消息循环卡死时放弃等待
	}
	m.mu.Lock()
}
