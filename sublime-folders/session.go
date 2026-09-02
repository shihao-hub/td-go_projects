package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

const (
	autoSaveName = "Auto Save Session.sublime_session"
	sessionName  = "Session.sublime_session"
)

type SessionFolder struct {
	Path string `json:"path"`
}

type SessionWindow struct {
	Folders []SessionFolder `json:"folders"`
	Project string          `json:"project"`
}

type SessionData struct {
	Windows []SessionWindow `json:"windows"`
}

func candidateSessionFiles() []string {
	appData := os.Getenv("APPDATA")
	dirs := []string{
		filepath.Join(appData, "Sublime Text", "Local"),
		filepath.Join(appData, "Sublime Text 3", "Local"),
	}
	var out []string
	for _, d := range dirs {
		out = append(out,
			filepath.Join(d, autoSaveName),
			filepath.Join(d, sessionName),
		)
	}
	return out
}

// LoadAutoSession tries candidates in order: live Auto Save first, then
// the exit-time snapshot, newest Sublime version dir first. A candidate
// that fails to read or parse is skipped so a half-written Auto Save
// file never breaks the run.
func LoadAutoSession() (*SessionData, string, error) {
	var lastErr error
	for _, c := range candidateSessionFiles() {
		data, err := os.ReadFile(c)
		if err != nil {
			lastErr = err
			continue
		}
		var sess SessionData
		if err := json.Unmarshal(data, &sess); err != nil {
			lastErr = fmt.Errorf("%s: %w", c, err)
			continue
		}
		return &sess, c, nil
	}
	if lastErr == nil {
		lastErr = errors.New("没有找到任何 Sublime Text 会话文件")
	}
	return nil, "", lastErr
}

// CurrentFolders 返回全部窗口目录的合并去重排序列表。
// 绑定了 .sublime-project 的窗口若会话里没有 folders，则回退读工程文件。
func CurrentFolders() ([]string, string, error) {
	sess, src, err := LoadAutoSession()
	if err != nil {
		return nil, "", err
	}
	set := map[string]bool{}
	for _, w := range sess.Windows {
		folders := w.Folders
		if len(folders) == 0 && w.Project != "" {
			if data, err := os.ReadFile(w.Project); err == nil {
				var proj struct {
					Folders []SessionFolder `json:"folders"`
				}
				if json.Unmarshal(data, &proj) == nil {
					folders = proj.Folders
				}
			}
		}
		for _, f := range folders {
			p := f.Path
			if p == "" {
				continue
			}
			if !filepath.IsAbs(p) && w.Project != "" {
				p = filepath.Join(filepath.Dir(w.Project), p)
			}
			set[p] = true
		}
	}
	out := make([]string, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Strings(out)
	return out, src, nil
}
