package report

import (
	"time"

	"projstat/internal/scan"
)

// EntryJSON 是 --json 输出的单项目视图；三态布尔用 *bool 自然序列化为 true/false/null。
type EntryJSON struct {
	Dir          string `json:"dir"`
	Name         string `json:"name"`
	Lang         string `json:"lang"`
	Stage        string `json:"stage"`
	SourceRead   *bool  `json:"source_read"`
	Usable       *bool  `json:"usable"`
	Tested       *bool  `json:"tested"`
	Summary      string `json:"summary"`
	NextAction   string `json:"next_action"`
	NextDue      string `json:"next_due"`
	UpdatedAt    string `json:"updated_at"`
	LastCommitAt string `json:"last_commit_at"` // RFC3339 或 ""
	Tag          string `json:"tag"`
	Overdue      bool   `json:"overdue"`
}

// EntryToJSON 将 scan.Entry 转换为 JSON 视图；未标注项目（Meta==nil）输出零值字段。
func EntryToJSON(e scan.Entry) EntryJSON {
	j := EntryJSON{
		Dir:  e.Dir,
		Name: e.Name,
		Lang: e.Lang,
	}
	if e.Meta != nil {
		j.Stage = string(e.Meta.Stage)
		j.SourceRead = e.Meta.SourceRead
		j.Usable = e.Meta.Usable
		j.Tested = e.Meta.Tested
		j.Summary = e.Meta.Summary
		j.NextAction = e.Meta.NextAction
		j.NextDue = e.Meta.NextDue
		j.UpdatedAt = e.Meta.UpdatedAt
	}
	if !e.Git.LastCommitAt.IsZero() {
		j.LastCommitAt = e.Git.LastCommitAt.Format(time.RFC3339)
	}
	j.Tag = e.Git.Tag
	j.Overdue = IsOverdue(j.NextDue)
	return j
}
