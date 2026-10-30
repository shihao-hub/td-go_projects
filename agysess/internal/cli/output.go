package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"agysess/internal/model"
)

// PrintJSON 输出标准成功 JSON 包络
func PrintJSON(w io.Writer, data any) {
	resp := model.ResponseEnvelope{
		OK:   true,
		Data: data,
	}
	bytes, _ := json.MarshalIndent(resp, "", "  ")
	fmt.Fprintln(w, string(bytes))
}

// PrintJSONError 输出标准失败 JSON 包络
func PrintJSONError(w io.Writer, code string, msg string) {
	resp := model.ResponseEnvelope{
		OK: false,
		Error: &model.ErrorEnvelope{
			Code:    code,
			Message: msg,
		},
	}
	bytes, _ := json.MarshalIndent(resp, "", "  ")
	fmt.Fprintln(w, string(bytes))
}

// FormatTimeRelative 格式化友好相对时间
func FormatTimeRelative(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	d := time.Since(t)
	if d < time.Minute {
		return "刚刚"
	}
	if d < time.Hour {
		return fmt.Sprintf("%d分钟前", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%d小时前", int(d.Hours()))
	}
	if d < 30*24*time.Hour {
		return fmt.Sprintf("%d天前", int(d.Hours()/24))
	}
	return t.Format("2006-01-02 15:04")
}

// TruncateString 截断长字符串
func TruncateString(s string, maxLen int) string {
	runes := []rune(strings.ReplaceAll(s, "\n", " "))
	if len(runes) <= maxLen {
		return string(runes)
	}
	return string(runes[:maxLen-3]) + "..."
}

// RenderSessionTable 人读模式格式化渲染会话列表
func RenderSessionTable(w io.Writer, sessions []model.Session, stats model.SessionStats) {
	if len(sessions) == 0 {
		fmt.Fprintln(w, "未找到匹配的会话。")
		fmt.Fprintf(w, "\n[统计] 全部: %d | 活跃: %d | 已归档: %d | 运行中: %d\n",
			stats.Total, stats.Active, stats.Archived, stats.Running)
		return
	}

	fmt.Fprintf(w, "%-10s  %-10s  %-30s  %-8s  %-10s  %-15s\n",
		"状态", "ID", "标题 / 备注", "步数", "更新时间", "项目")
	fmt.Fprintln(w, strings.Repeat("-", 92))

	for _, s := range sessions {
		statusStr := "活跃"
		if s.IsRunning {
			statusStr = fmt.Sprintf("运行:%d", s.PID)
		} else if s.IsArchived {
			statusStr = "已归档"
		}

		shortID := s.ID
		if len(shortID) > 8 {
			shortID = shortID[:8]
		}

		title := s.DisplayTitle()
		title = TruncateString(title, 28)

		timeStr := FormatTimeRelative(s.LastModifiedTime)
		project := s.ProjectID
		if project == "" {
			project = "-"
		}
		project = TruncateString(project, 14)

		fmt.Fprintf(w, "%-10s  %-10s  %-30s  %-8d  %-10s  %-15s\n",
			statusStr, shortID, title, s.StepCount, timeStr, project)
	}

	fmt.Fprintln(w, strings.Repeat("-", 92))
	fmt.Fprintf(w, "显示 %d 条记录 (全部: %d | 活跃: %d | 已归档: %d | 运行中: %d)\n",
		len(sessions), stats.Total, stats.Active, stats.Archived, stats.Running)
}

// RenderSessionDetail 人读模式格式化渲染单会话详情
func RenderSessionDetail(w io.Writer, s *model.Session) {
	fmt.Fprintf(w, "会话 ID:       %s\n", s.ID)
	fmt.Fprintf(w, "原生标题:     %s\n", s.Title)
	if s.CustomTitle != "" {
		fmt.Fprintf(w, "自定义标题:   %s\n", s.CustomTitle)
	}
	if s.CustomNotes != "" {
		fmt.Fprintf(w, "备注信息:     %s\n", s.CustomNotes)
	}
	fmt.Fprintf(w, "运行状态:     ")
	if s.IsRunning {
		fmt.Fprintf(w, "正在运行 (PID: %d)\n", s.PID)
	} else {
		fmt.Fprintln(w, "未运行")
	}
	fmt.Fprintf(w, "归档状态:     ")
	if s.IsArchived {
		if s.ArchivedAt != nil {
			fmt.Fprintf(w, "已归档 (归档时间: %s)\n", s.ArchivedAt.Format("2006-01-02 15:04:05"))
		} else {
			fmt.Fprintln(w, "已归档")
		}
	} else {
		fmt.Fprintln(w, "正常")
	}
	fmt.Fprintf(w, "交互步数:     %d\n", s.StepCount)
	fmt.Fprintf(w, "所属项目:     %s\n", s.ProjectID)
	fmt.Fprintf(w, "工作区路径:   %s\n", s.WorkspaceURIs)
	fmt.Fprintf(w, "最后更新:     %s (%s)\n",
		s.LastModifiedTime.Format("2006-01-02 15:04:05"),
		FormatTimeRelative(s.LastModifiedTime))
	if s.Preview != "" {
		fmt.Fprintf(w, "\n--- 会话预览 ---\n%s\n", s.Preview)
	}
}
