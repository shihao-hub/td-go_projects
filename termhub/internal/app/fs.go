// fs.go：目录浏览 API，为前端「新建终端」提供初始工作目录选择。
package app

import (
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

type fsDirEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type fsListing struct {
	// Path 当前目录；空字符串表示「此电脑」驱动器列表。
	Path string `json:"path"`
	// Parent 上一级目录；空字符串表示再往上回到驱动器列表。
	Parent string       `json:"parent"`
	Home   string       `json:"home"`
	Dirs   []fsDirEntry `json:"dirs"`
}

// handleListDirs GET /api/v1/fs/list?path=
func (s *Server) handleListDirs(w http.ResponseWriter, r *http.Request) {
	home, _ := os.UserHomeDir()
	raw := strings.TrimSpace(r.URL.Query().Get("path"))
	raw = strings.Trim(raw, `"'`)

	if raw == "" {
		writeJSON(w, http.StatusOK, APIResponse{OK: true, Data: fsListing{
			Home: home,
			Dirs: listRoots(),
		}})
		return
	}

	raw = expandHome(raw, home)
	// Windows 下 "C:" 会被解释为该盘的当前目录，补成根目录更符合预期。
	if runtime.GOOS == "windows" && len(raw) == 2 && raw[1] == ':' {
		raw += `\`
	}

	abs, err := filepath.Abs(filepath.Clean(raw))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{OK: false, Error: "路径无效: " + err.Error()})
		return
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		writeJSON(w, http.StatusBadRequest, APIResponse{OK: false, Error: "目录不存在: " + abs})
		return
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{OK: false, Error: "无法读取目录: " + err.Error()})
		return
	}

	dirs := make([]fsDirEntry, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dirs = append(dirs, fsDirEntry{
			Name: entry.Name(),
			Path: filepath.Join(abs, entry.Name()),
		})
	}
	sort.Slice(dirs, func(i, j int) bool {
		return strings.ToLower(dirs[i].Name) < strings.ToLower(dirs[j].Name)
	})

	parent := filepath.Dir(abs)
	if parent == abs {
		parent = "" // 盘符根目录的上一级是「此电脑」
	}
	writeJSON(w, http.StatusOK, APIResponse{OK: true, Data: fsListing{
		Path:   abs,
		Parent: parent,
		Home:   home,
		Dirs:   dirs,
	}})
}

// expandHome 展开 "~" / "~/x" / "~\x" 为当前用户主目录。
func expandHome(path, home string) string {
	if home == "" {
		return path
	}
	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, `~\`) || strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:])
	}
	return path
}

// listRoots 列出可访问的根：Windows 为各驱动器，其它平台为 "/"。
func listRoots() []fsDirEntry {
	if runtime.GOOS != "windows" {
		return []fsDirEntry{{Name: "/", Path: "/"}}
	}
	out := make([]fsDirEntry, 0, 8)
	for letter := 'A'; letter <= 'Z'; letter++ {
		root := string(letter) + `:\`
		info, err := os.Stat(root)
		if err != nil || !info.IsDir() {
			continue
		}
		out = append(out, fsDirEntry{Name: string(letter) + ":", Path: root})
	}
	return out
}
