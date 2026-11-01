package pool

import (
	"fmt"
	"os"
	"strings"
)

// AGYProcess 描述一个 AGY 侧进程（主服务 agy_acp_server 或它的 local harness）。
type AGYProcess struct {
	PID      int    `json:"pid"`
	Name     string `json:"name"`
	Parent   int    `json:"parent_pid"`
	MemoryMB int    `json:"memory_mb"`
	Created  string `json:"created_at"`
}

// IsAGYProcess 判断进程名是否属于 AGY 进程树。
// 这两个进程都会常驻约 12MB / 120MB 以上，父进程退出后不会自杀，是内存泄漏的主要来源。
func IsAGYProcess(name string) bool {
	return strings.EqualFold(name, "agy_acp_server.exe") ||
		strings.EqualFold(name, "localharness_external.exe")
}

// ReapResult 是一次孤儿回收的统计结果。
type ReapResult struct {
	Scanned  int          `json:"scanned"`
	Orphans  []AGYProcess `json:"orphans"`
	Reaped   int          `json:"reaped"`
	FreedMB  int          `json:"freed_mb"`
	DryRun   bool         `json:"dry_run"`
	Failures []string     `json:"failures,omitempty"`
}

// Logf 输出一次回收摘要；logger 为 nil 时打印到 stderr。
func (r ReapResult) Logf(logger interface{ Printf(string, ...any) }) {
	if len(r.Orphans) == 0 {
		return
	}
	msg := fmt.Sprintf("[OrphanReaper] 发现 %d 个孤儿 AGY 进程（父进程已退出），合计约 %d MB", len(r.Orphans), r.FreedMB)
	if r.DryRun {
		msg += "（dry-run，未清理）"
	} else {
		msg += fmt.Sprintf("，已清理 %d 个", r.Reaped)
	}
	if logger != nil {
		logger.Printf(msg)
	} else {
		fmt.Fprintln(os.Stderr, msg)
	}
	for _, p := range r.Orphans {
		line := fmt.Sprintf("%s pid=%d parent=%d mem=%dMB created=%s", p.Name, p.PID, p.Parent, p.MemoryMB, p.Created)
		if logger != nil {
			logger.Printf("  orphan: %s", line)
		} else {
			fmt.Fprintln(os.Stderr, "  orphan:", line)
		}
	}
}
