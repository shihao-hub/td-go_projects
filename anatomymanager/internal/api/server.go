package api

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"strings"

	"anatomymanager/internal/model"
	"anatomymanager/internal/storage"
	"anatomymanager/internal/web"
)

type Server struct {
	store *storage.Store
	meta  Meta
}

// Meta 描述当前服务实例所属的构建通道与数据落点，
// 经 /api/meta 暴露给 WebUI，让用户能一眼确认「这个窗口看的是哪个库」。
type Meta struct {
	Channel   string `json:"channel"`
	BuildMode string `json:"buildMode"`
	Version   string `json:"version"`
	DBPath    string `json:"dbPath"`
}

func NewServer(store *storage.Store, meta Meta) *Server {
	return &Server{store: store, meta: meta}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/meta", s.handleMeta)
	mux.HandleFunc("/api/artworks", s.handleArtworks)
	mux.HandleFunc("/api/artworks/", s.handleArtworkResource)
	subFS, err := fs.Sub(web.Assets, "assets")
	if err == nil {
		mux.Handle("/", http.FileServer(http.FS(subFS)))
	}
	return mux
}

// Listen 在回环地址上绑定监听器；port 传 0 表示由内核动态探测可用端口
// （dev / release 等多个实例并行时天然互不抢占，无需端口约定）。
func Listen(port int) (net.Listener, error) {
	return net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
}

// URL 由监听器推导出实际可访问地址，供宿主（Wails 窗口、托盘、CLI 输出）使用。
func URL(listener net.Listener) string {
	return "http://" + listener.Addr().String()
}

// Serve 在调用方已绑定的监听器上提供服务。宿主需要先拿到真实端口再构造
// 窗口地址（如 Wails 桌面壳）时必须走该入口，而不是 Start。
func (s *Server) Serve(listener net.Listener) error {
	return http.Serve(listener, s.Handler())
}

// Start 绑定端口并阻塞提供服务，返回实际访问地址（port=0 时为动态端口）。
func (s *Server) Start(port int, openBrowser bool) (string, error) {
	listener, err := Listen(port)
	if err != nil {
		return "", fmt.Errorf("绑定监听失败: %w", err)
	}
	url := URL(listener)
	fmt.Printf("Anatomy Manager WebUI is running at %s\n", url)

	if openBrowser {
		go openURL(url)
	}

	return url, s.Serve(listener)
}

func (s *Server) handleMeta(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	payload := struct {
		Meta
		Artworks int `json:"artworks"`
		Files    int `json:"files"`
	}{Meta: s.meta}
	if artworks, files, err := s.store.Stats(); err == nil {
		payload.Artworks, payload.Files = artworks, files
	}
	writeJSON(w, payload)
}

func (s *Server) handleArtworks(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		list, err := s.store.ListArtworks()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if list == nil {
			list = []model.Artwork{}
		}
		writeJSON(w, list)
		return
	}
	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

func (s *Server) handleArtworkResource(w http.ResponseWriter, r *http.Request) {
	// Path format: /api/artworks/{id} or /api/artworks/{id}/files/{filename}
	path := strings.TrimPrefix(r.URL.Path, "/api/artworks/")
	parts := strings.Split(path, "/")
	if len(parts) == 0 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}

	artworkID := parts[0]

	// 1. /api/artworks/{id}
	if len(parts) == 1 {
		if r.Method == http.MethodGet {
			art, err := s.store.GetArtwork(artworkID)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			writeJSON(w, art)
			return
		}
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 2. /api/artworks/{id}/files/{filename}
	if len(parts) >= 3 && parts[1] == "files" {
		fileName := strings.Join(parts[2:], "/")
		f, err := s.store.GetFile(artworkID, fileName)
		if err != nil {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Content-Type", f.MimeType)
		w.Header().Set("Content-Length", fmt.Sprintf("%d", f.SizeBytes))
		// Cache control for performance
		w.Header().Set("Cache-Control", "public, max-age=3600")
		_, _ = w.Write(f.ContentBlob)
		return
	}

	http.NotFound(w, r)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

func openURL(url string) {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", url}
	case "darwin":
		cmd = "open"
		args = []string{url}
	default:
		cmd = "xdg-open"
		args = []string{url}
	}
	_ = exec.Command(cmd, args...).Start()
}

// ReadRequestBody helper
func readBody(r *http.Request) ([]byte, error) {
	defer r.Body.Close()
	return io.ReadAll(r.Body)
}
