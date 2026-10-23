// instance.go：自研单实例接力——Windows 命名互斥体判存 + instance.json 交接。
// 背景：Wails v3 beta.34 的 SingleInstance 命名管道在常驻/重启场景会使新实例
// 无日志、无窗口地卡死在 application.New 内；这里用进程级互斥体（自动随进程
// 释放）加 HTTP 探测交接，新实例唤出既有窗口后立即退出，全程可在日志观测。
//
// 两类退出竞态：
//  1. 正常退出：Windows 在 ExitProcess 阶段先终止其它线程、再关闭句柄，期间
//     「HTTP 已死、互斥体仍被占」可持续数秒；此时重启会被朴素实现判定为
//     「实例不可达」并静默退出。这里改为带宽限期的轮询，等旧进程释放后接手。
//  2. 异常退出：WebView2 的 DllMain 可能让 os.Exit/ExitProcess 永久挂起，
//     进程只剩一个线程、互斥体永不释放，此后每次启动都无窗口。宽限期后若
//     登记 PID 仍是本程序映像，询问用户是否结束该残留进程再启动。
package app

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows"
)

const (
	instanceFileName = "instance.json"
	// instanceRetryInterval 轮询既有实例释放互斥体的间隔。
	instanceRetryInterval = 200 * time.Millisecond
	// instanceRetryTimeout 等待旧进程完成退出的宽限期；覆盖 WebView2 清理耗时。
	instanceRetryTimeout = 6 * time.Second
	// reclaimRetryTimeout 用户确认结束残留进程后，等待互斥体释放的短宽限。
	reclaimRetryTimeout = 3 * time.Second
	// aliveProbeTimeout 存活探测超时；轮询期间必须短，避免拖长宽限期。
	aliveProbeTimeout = 700 * time.Millisecond
	// idYes MessageBox IDYES。
	idYes = 6
)

// instanceMutex 互斥体句柄须进程级持有，句柄丢失即释放所有权。
var instanceMutex windows.Handle

type instanceRecord struct {
	URL string `json:"url"`
	PID int    `json:"pid"`
}

// acquireInstance 尝试成为唯一实例：
//   - (true, "", false)：取得互斥体，可以作为首个实例运行；
//   - (false, url, false)：已有实例驻留且可交接，url 是它的本地服务地址；
//   - (false, _, true)：宽限期内互斥体始终被占且实例不可达（旧进程卡在退出中）。
func acquireInstance(dataDir, channel string) (bool, string, bool) {
	return acquireInstanceWithin(dataDir, channel, instanceRetryTimeout)
}

// acquireInstanceWithin 以指定宽限期执行判存轮询。
func acquireInstanceWithin(dataDir, channel string, grace time.Duration) (bool, string, bool) {
	name, err := windows.UTF16PtrFromString("Local\\termhub." + channel)
	if err != nil {
		// 名称构造失败退化为无条件首实例（自检日志路径不可见）。
		return true, "", false
	}
	return pollInstanceAcquire(grace, instanceRetryInterval, func() (bool, string, bool) {
		return tryAcquireMutex(name, dataDir)
	})
}

// tryAcquireMutex 单次判存；already-exists 时顺手关闭返回句柄，避免轮询泄漏。
//
// 命名对象存在不等于有活主：ExitProcess 卡在 DllMain 的进程会把互斥体留成
// 「句柄未销毁、线程已终止」，此时对象永远等不到释放。零超时 Wait 可区分：
//   - WAIT_TIMEOUT：仍有活线程持有 → 既有实例在运行；
//   - WAIT_OBJECT_0 / WAIT_ABANDONED：无人持有（首次创建或被遗弃）→ 本实例
//     取得所有权接管，后续实例才会重新看到「有活主」。
func tryAcquireMutex(name *uint16, dataDir string) (acquired bool, url string, alive bool) {
	h, err := windows.CreateMutex(nil, true, name)
	if h == 0 {
		// 创建失败：互斥体不可用仍按首实例运行，互斥仅是防重兜底。
		return true, "", false
	}
	if err != windows.ERROR_ALREADY_EXISTS {
		instanceMutex = h
		return true, "", false
	}

	// 对象已存在：本句柄不带所有权，用零超时等待试探是否有人持有。
	ev, waitErr := windows.WaitForSingleObject(h, 0)
	if waitErr == nil && (ev == windows.WAIT_OBJECT_0 || ev == windows.WAIT_ABANDONED) {
		// 无人持有 → 已取得所有权。旧版实例（未持所有权）仍可能健在，先探活。
		url = readInstanceURL(dataDir)
		if url != "" && probeAlive(url) {
			_ = windows.CloseHandle(h)
			return false, url, true
		}
		instanceMutex = h
		return true, "", false
	}
	_ = windows.CloseHandle(h)

	url = readInstanceURL(dataDir)
	return false, url, url != "" && probeAlive(url)
}

// pollInstanceAcquire 在宽限期内轮询 attempt，直到取得互斥体或探到可交接实例。
func pollInstanceAcquire(grace, interval time.Duration, attempt func() (bool, string, bool)) (primary bool, url string, busy bool) {
	deadline := time.Now().Add(grace)
	for {
		acquired, existingURL, alive := attempt()
		if acquired {
			return true, "", false
		}
		if alive {
			return false, existingURL, false
		}
		if !time.Now().Before(deadline) {
			return false, existingURL, true
		}
		time.Sleep(interval)
	}
}

// probeAlive 探测既有实例的本地服务是否仍在应答。
func probeAlive(url string) bool {
	client := &http.Client{Timeout: aliveProbeTimeout}
	resp, err := client.Get(url + "/api/v1/alive")
	if err != nil {
		return false
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// handoffToExisting 唤出既有实例的窗口；成功返回 true（并应退出自身）。
func handoffToExisting(url string) bool {
	if !probeAlive(url) {
		return false
	}
	client := &http.Client{Timeout: 1500 * time.Millisecond}
	show, err := client.Post(url+"/api/v1/ui/show", "application/json", nil)
	if err != nil {
		return true // 窗口唤出请求失败，但实例已确认存活，仍退出新实例。
	}
	_, _ = io.Copy(io.Discard, show.Body)
	_ = show.Body.Close()
	return true
}

// reclaimStaleInstance 宽限期后仍被占且不可达：向用户确认后结束登记的残留进程。
// 只有 PID 仍指向本程序映像时才动手，避免 PID 复用误伤其它程序。
func reclaimStaleInstance(dataDir string) bool {
	rec := readInstanceRecord(dataDir)
	if rec.PID <= 0 || !confirmReclaim(rec.PID) {
		return false
	}
	return terminateIfSelfImage(rec.PID)
}

// confirmReclaim 让用户决定是否结束无响应的旧进程。
func confirmReclaim(pid int) bool {
	text, _ := windows.UTF16PtrFromString(fmt.Sprintf(
		"检测到 termhub 上一次运行未能正常退出（进程 %d 已无响应，窗口也已消失）。\n\n"+
			"结束该残留进程并重新启动？\n\n"+
			"选择「否」将取消本次启动；可在任务管理器中手动结束 termhub 后再试。", pid))
	caption, _ := windows.UTF16PtrFromString("termhub")
	ret, _ := windows.MessageBox(0, text, caption,
		windows.MB_YESNO|windows.MB_ICONWARNING|windows.MB_SETFOREGROUND|windows.MB_TOPMOST)
	return ret == idYes
}

// terminateIfSelfImage 结束与当前可执行文件同名的进程。
func terminateIfSelfImage(pid int) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)

	buf := make([]uint16, windows.MAX_PATH)
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &size); err != nil {
		return false
	}
	self, err := os.Executable()
	if err != nil {
		return false
	}
	if !strings.EqualFold(filepath.Base(windows.UTF16ToString(buf[:size])), filepath.Base(self)) {
		return false
	}
	return windows.TerminateProcess(h, 1) == nil
}

// hardExit 结束进程但不进入 ExitProcess 的 DLL 卸载流程。
// Wails beta + WebView2 可能在 DllMain 中死锁，使 os.Exit 永久挂起：
// 进程只剩一个线程、数据已收尾但互斥体句柄不释放，表现为「托盘残留、
// 再次启动无窗口」。TerminateProcess 不执行 DllMain，可确保进程真正消失。
func hardExit() {
	_ = windows.TerminateProcess(windows.CurrentProcess(), 0)
	os.Exit(0)
}

// notifyInstanceBusy 静默退出会让用户误以为程序损坏；用系统消息框点明原因。
func notifyInstanceBusy() {
	text, _ := windows.UTF16PtrFromString("检测到 termhub 上一次运行仍在退出（或已失去响应），本次启动已取消。\n请稍候几秒后重新启动；若仍无法启动，请在任务管理器中结束 termhub 进程。")
	caption, _ := windows.UTF16PtrFromString("termhub")
	_, _ = windows.MessageBox(0, text, caption,
		windows.MB_OK|windows.MB_ICONINFORMATION|windows.MB_SETFOREGROUND|windows.MB_TOPMOST)
}

// readInstanceRecord 读取既有实例登记信息；损坏/缺失返回零值。
func readInstanceRecord(dir string) instanceRecord {
	data, err := os.ReadFile(filepath.Join(dir, instanceFileName))
	if err != nil {
		return instanceRecord{}
	}
	var rec instanceRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return instanceRecord{}
	}
	return rec
}

// readInstanceURL 读取既有实例登记的服务地址；损坏/缺失返回空。
func readInstanceURL(dir string) string {
	return readInstanceRecord(dir).URL
}

// writeInstanceFile 登记本场实例的服务地址（URL 过期无害，仅作交接参考）。
func writeInstanceFile(dir, url string) error {
	rec := instanceRecord{URL: url, PID: os.Getpid()}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, instanceFileName), append(data, '\n'), 0o644)
}
