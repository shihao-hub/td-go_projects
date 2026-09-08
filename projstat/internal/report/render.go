// Package report 负责三种视图（表格/详情/待办）的渲染：CJK 对齐、ANSI 颜色管理、相对时间与 JSON 信封。
package report

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"golang.org/x/text/width"

	"projstat/internal/scan"
)

// UseColor 由 main 在启动时判定：stdout 为字符设备且非 --json 时为 true。
// 为 false 时所有 ANSI 颜色函数退化为原样返回，保证管道/文件输出干净。
var UseColor bool

// runeWidth 返回单个 rune 的终端显示宽度（CJK 宽字符为 2）。
func runeWidth(r rune) int {
	switch width.LookupRune(r).Kind() {
	case width.EastAsianWide, width.EastAsianFullwidth:
		return 2
	default:
		return 1
	}
}

// Width 计算字符串的终端显示宽度，按 CJK 宽度规则逐 rune 累加。
// 注意：字符串含 ANSI 转义序列时结果不准确，调用方应先剥离或自行记录内容宽。
func Width(s string) int {
	w := 0
	for _, r := range s {
		w += runeWidth(r)
	}
	return w
}

// Pad 将字符串按显示宽度补空格到 w；超宽时原样返回不截断。
func Pad(s string, w int) string {
	d := w - Width(s)
	if d <= 0 {
		return s
	}
	return s + strings.Repeat(" ", d)
}

// Truncate 按显示宽截断字符串到 w，截断时以省略号结尾。
func Truncate(s string, w int) string {
	if Width(s) <= w {
		return s
	}
	var b strings.Builder
	cur := 0
	for _, r := range s {
		rw := runeWidth(r)
		if cur+rw > w-1 { // 留 1 列给省略号
			break
		}
		b.WriteRune(r)
		cur += rw
	}
	return b.String() + "…"
}

// Red 将文本包红色 ANSI；UseColor 为 false 时原样返回。
func Red(s string) string {
	if !UseColor {
		return s
	}
	return "\x1b[31m" + s + "\x1b[0m"
}

// RelTime 输出人类可读的相对时间："<1h" "3h" "2d" "3w" "1y"；未来与过去同样处理；零值返回 "-"。
func RelTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	abs := time.Since(t)
	if abs < 0 {
		abs = -abs
	}
	switch {
	case abs < time.Hour:
		return "<1h"
	case abs < 24*time.Hour:
		return fmt.Sprintf("%dh", int64(abs.Hours()))
	case abs < 7*24*time.Hour:
		return fmt.Sprintf("%dd", int64(abs.Hours()/24))
	case abs < 365*24*time.Hour:
		return fmt.Sprintf("%dw", int64(abs.Hours()/(24*7)))
	default:
		return fmt.Sprintf("%dy", int64(abs.Hours()/(24*365)))
	}
}

// IsOverdue 判断 next_due 是否已逾期：非空且早于今天（当天到期不算逾期）。
func IsOverdue(due string) bool {
	if due == "" {
		return false
	}
	t, err := time.Parse("2006-01-02", due)
	if err != nil {
		return false
	}
	today, _ := time.Parse("2006-01-02", time.Now().Format("2006-01-02"))
	return t.Before(today)
}

// cell 是表格单元格：text 可含 ANSI 颜色码，w 是剥离 ANSI 后的内容显示宽。
// 颜色码不计入宽度，从而保证对齐与着色解耦（本项目最大风险点的消解方式）。
type cell struct {
	text string
	w    int
}

func newCell(s string) cell { return cell{text: s, w: Width(s)} }
func newCellC(s string) cell { // 预着色单元格：宽度按原始文本计
	return cell{text: Red(s), w: Width(s)}
}

// renderTable 通用表格渲染：自动按列内最大显示宽对齐，列间两空格。
func renderTable(header []string, rows [][]cell) string {
	n := len(header)
	colW := make([]int, n)
	for i, h := range header {
		colW[i] = Width(h)
	}
	for _, row := range rows {
		for i, c := range row {
			if i < n && c.w > colW[i] {
				colW[i] = c.w
			}
		}
	}
	var b strings.Builder
	for i, h := range header {
		b.WriteString(Pad(h, colW[i]))
		if i < n-1 {
			b.WriteString("  ")
		}
	}
	b.WriteString("\n")
	for i := range header {
		b.WriteString(strings.Repeat("-", colW[i]))
		if i < n-1 {
			b.WriteString("  ")
		}
	}
	b.WriteString("\n")
	for _, row := range rows {
		for i := 0; i < n; i++ {
			if i < len(row) {
				c := row[i]
				b.WriteString(c.text)
				if d := colW[i] - c.w; d > 0 {
					b.WriteString(strings.Repeat(" ", d))
				}
			} else {
				b.WriteString(strings.Repeat(" ", colW[i]))
			}
			if i < n-1 {
				b.WriteString("  ")
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}

// tri 将三态布尔渲染为 ✓/✗/-（nil = 未标注）。
func tri(v *bool) string {
	if v == nil {
		return "-"
	}
	if *v {
		return "✓"
	}
	return "✗"
}

func stageCell(e scan.Entry) string {
	if e.Meta == nil || e.Meta.Stage == "" {
		return "-"
	}
	return string(e.Meta.Stage)
}

func dueCell(e scan.Entry) cell {
	if e.Meta == nil || e.Meta.NextDue == "" {
		return newCell("-")
	}
	if IsOverdue(e.Meta.NextDue) {
		return newCellC(e.Meta.NextDue)
	}
	return newCell(e.Meta.NextDue)
}

// Table 渲染 list 视图：NAME|LANG|STAGE|SRC|USE|TST|COMMIT|NEXT_DUE。
func Table(es []scan.Entry) string {
	header := []string{"NAME", "LANG", "STAGE", "SRC", "USE", "TST", "COMMIT", "NEXT_DUE"}
	rows := make([][]cell, 0, len(es))
	for _, e := range es {
		src, use, tst := "-", "-", "-"
		if e.Meta != nil {
			src, use, tst = tri(e.Meta.SourceRead), tri(e.Meta.Usable), tri(e.Meta.Tested)
		}
		rows = append(rows, []cell{
			newCell(e.Name),
			newCell(e.Lang),
			newCell(stageCell(e)),
			newCell(src),
			newCell(use),
			newCell(tst),
			newCell(RelTime(e.Git.LastCommitAt)),
			dueCell(e),
		})
	}
	return renderTable(header, rows)
}

// NextTable 渲染 next 待办视图：NAME|LANG|STAGE|DUE|ACTION，action 截断到 40 显示宽。
func NextTable(es []scan.Entry) string {
	header := []string{"NAME", "LANG", "STAGE", "DUE", "ACTION"}
	rows := make([][]cell, 0, len(es))
	for _, e := range es {
		rows = append(rows, []cell{
			newCell(e.Name),
			newCell(e.Lang),
			newCell(stageCell(e)),
			dueCell(e),
			newCell(Truncate(e.Meta.NextAction, 40)),
		})
	}
	return renderTable(header, rows)
}

// Show 渲染单项目详情：全部手工字段 + git 采集小节（最后提交时间、最近 tag）。
func Show(e scan.Entry) string {
	var b strings.Builder
	kv := func(k string, v string) {
		b.WriteString(Pad(k, 12))
		b.WriteString(v)
		b.WriteString("\n")
	}
	var m *scan.Meta
	if e.Meta != nil {
		m = e.Meta
	}
	if m == nil {
		kv("name", e.Name)
		kv("lang", e.Lang)
		kv("stage", "-")
		kv("source_read", "-")
		kv("usable", "-")
		kv("tested", "-")
		kv("summary", "-")
		kv("next_action", "-")
		kv("next_due", "-")
		kv("notes", "-")
		kv("updated_at", "-")
	} else {
		due := m.NextDue
		if due == "" {
			due = "-"
		} else if IsOverdue(due) {
			due = Red(due + " (overdue)")
		}
		kv("name", m.Name)
		kv("lang", m.Lang)
		kv("stage", stageCell(e))
		kv("source_read", tri(m.SourceRead))
		kv("usable", tri(m.Usable))
		kv("tested", tri(m.Tested))
		kv("summary", m.Summary)
		kv("next_action", m.NextAction)
		kv("next_due", due)
		kv("notes", m.Notes)
		kv("updated_at", m.UpdatedAt)
	}
	b.WriteString("\n-- git --\n")
	kv("dir", e.Dir)
	if e.Git.LastCommitAt.IsZero() {
		kv("last_commit", "-")
	} else {
		kv("last_commit", e.Git.LastCommitAt.Format("2006-01-02 15:04:05")+" ("+RelTime(e.Git.LastCommitAt)+")")
	}
	tag := e.Git.Tag
	if tag == "" {
		tag = "-"
	}
	kv("tag", tag)
	return b.String()
}

// Envelope 组装成功 JSON 信封：{"status":"ok","data":…,"count":n,"elapsed_ms":x}，单行无颜色。
func Envelope(data any, count int, elapsed time.Duration) string {
	type envelope struct {
		Status    string `json:"status"`
		Data      any    `json:"data"`
		Count     int    `json:"count"`
		ElapsedMs int64  `json:"elapsed_ms"`
	}
	return marshalLine(envelope{Status: "ok", Data: data, Count: count, ElapsedMs: elapsed.Milliseconds()})
}

// ErrorEnvelope 组装失败 JSON 信封：{"status":"error","message":…}。
func ErrorEnvelope(msg string) string {
	type errEnvelope struct {
		Status  string `json:"status"`
		Message string `json:"message"`
	}
	return marshalLine(errEnvelope{Status: "error", Message: msg})
}

// marshalLine 输出单行 JSON（关闭 HTML 转义、去掉尾换行）。
func marshalLine(v any) string {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return `{"status":"error","message":"json encode failed"}`
	}
	return strings.TrimRight(b.String(), "\n")
}
