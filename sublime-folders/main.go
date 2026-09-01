// sublime-folders: list the folders currently opened in Sublime Text
// windows, by reading Sublime's session files on disk (read-only).
//
// Usage:
//   sublime-folders.exe              print folders per window, wait for Enter before exit
//   sublime-folders.exe -no-pause    skip the "press Enter to exit" wait
//   sublime-folders.exe -json        machine-readable JSON output (implies -no-pause)
//   sublime-folders.exe -file PATH   read a specific .sublime_session file
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

const (
	autoSaveName = "Auto Save Session.sublime_session"
	sessionName  = "Session.sublime_session"
)

type sessionFolder struct {
	Path string `json:"path"`
}

type sessionWindow struct {
	Folders []sessionFolder `json:"folders"`
	Project string          `json:"project"`
}

type sessionData struct {
	Windows []sessionWindow `json:"windows"`
}

type windowResult struct {
	Project string   `json:"project,omitempty"`
	Folders []string `json:"folders"`
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

// loadAutoSession tries candidates in order: live Auto Save first, then
// the exit-time snapshot, newest Sublime version dir first. A candidate
// that fails to read or parse is skipped so a half-written Auto Save
// file never breaks the run.
func loadAutoSession() (*sessionData, string, error) {
	var lastErr error
	for _, c := range candidateSessionFiles() {
		data, err := os.ReadFile(c)
		if err != nil {
			lastErr = err
			continue
		}
		var sess sessionData
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

func collect(sess *sessionData) []windowResult {
	var out []windowResult
	for _, w := range sess.Windows {
		res := windowResult{Project: w.Project}
		folders := w.Folders
		// Untitled windows keep their folders in the session; windows
		// bound to a .sublime-project may only have them in the project
		// file, so fall back to reading it.
		if len(folders) == 0 && w.Project != "" {
			if data, err := os.ReadFile(w.Project); err == nil {
				var proj struct {
					Folders []sessionFolder `json:"folders"`
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
			res.Folders = append(res.Folders, p)
		}
		out = append(out, res)
	}
	return out
}

func printPretty(results []windowResult) {
	if len(results) == 0 {
		fmt.Println("会话里没有打开的窗口或文件夹。")
		return
	}
	for i, r := range results {
		label := "(untitled)"
		if r.Project != "" {
			label = filepath.Base(r.Project)
		}
		fmt.Printf("窗口 %d: %d 个文件夹  %s\n", i+1, len(r.Folders), label)
		for _, p := range r.Folders {
			fmt.Println("  " + p)
		}
	}
}

func setConsoleUTF8() {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	kernel32.NewProc("SetConsoleOutputCP").Call(65001)
}

func main() {
	jsonOut := flag.Bool("json", false, "output machine-readable JSON (implies -no-pause)")
	noPause := flag.Bool("no-pause", false, "do not wait for Enter before exit")
	file := flag.String("file", "", "path to a .sublime_session file (default: auto-detect)")
	flag.Parse()

	setConsoleUTF8()

	var (
		results []windowResult
		source  string
		err     error
	)
	if *file != "" {
		var data []byte
		data, err = os.ReadFile(*file)
		if err == nil {
			var sess sessionData
			err = json.Unmarshal(data, &sess)
			if err == nil {
				results = collect(&sess)
				source = *file
			}
		}
	} else {
		var sess *sessionData
		sess, source, err = loadAutoSession()
		if err == nil {
			results = collect(sess)
		}
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "错误:", err)
	} else {
		if results == nil {
			results = []windowResult{}
		}
		if *jsonOut {
			out, _ := json.MarshalIndent(results, "", "  ")
			fmt.Println(string(out))
		} else {
			fmt.Printf("会话文件: %s\n\n", source)
			printPretty(results)
		}
	}

	// Double-click launches have no argument control, so the default is
	// to pause and let the user read the output. Scripting goes through
	// -no-pause (or -json).
	if !*jsonOut && !*noPause {
		fmt.Print("\n按回车键退出...")
		bufio.NewReader(os.Stdin).ReadString('\n')
	}
}
